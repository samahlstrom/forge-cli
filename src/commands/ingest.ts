import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join, resolve, extname, basename } from 'node:path';
import { exists, readText, writeText, ensureDir } from '../utils/fs.js';
import { execaCommand } from 'execa';
import { copyFile, stat, readFile } from 'node:fs/promises';

interface IngestOptions {
	chunkSize?: string;
	resume?: string;
}

interface SpecAnalysis {
	project_name: string;
	description: string;
	language: string;
	framework: string | null;
	project_type: string;
	modules: string[];
	architecture: string;
	sensitive_areas: string;
	domain_rules: string;
	constraints: string[];
	page_count: number | null;
}

export async function ingest(files: string[], options: IngestOptions): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge') + chalk.dim(' — Spec Ingestion'));

	// Resolve and validate all spec files
	const resolvedFiles: { path: string; ext: string; name: string; sizeDisplay: string; sizeBytes: number; pageCount: number | null }[] = [];

	for (const file of files) {
		const specPath = resolve(file);
		if (!(await exists(specPath))) {
			p.cancel(`File not found: ${specPath}`);
			process.exit(1);
		}

		const ext = extname(specPath).toLowerCase();
		const supportedFormats = ['.pdf', '.md', '.txt', '.markdown'];
		if (!supportedFormats.includes(ext)) {
			p.cancel(`Unsupported format: ${ext} (${basename(specPath)}). Supported: ${supportedFormats.join(', ')}`);
			process.exit(1);
		}

		const fileStats = await stat(specPath);
		const sizeBytes = fileStats.size;
		const sizeMB = sizeBytes < 102400
			? `${(sizeBytes / 1024).toFixed(0)} KB`
			: `${(sizeBytes / (1024 * 1024)).toFixed(1)} MB`;

		let pageCount: number | null = null;
		if (ext === '.pdf') {
			pageCount = await detectPageCount(specPath);
		}

		resolvedFiles.push({ path: specPath, ext, name: basename(specPath), sizeDisplay: sizeMB, sizeBytes, pageCount });
	}

	const isMulti = resolvedFiles.length > 1;

	// Display source info
	const infoLines: string[] = [];
	if (isMulti) {
		infoLines.push(`Documents: ${chalk.cyan(String(resolvedFiles.length))}`);
		for (const f of resolvedFiles) {
			const format = f.ext === '.pdf' ? 'PDF' : f.ext === '.md' || f.ext === '.markdown' ? 'MD' : 'TXT';
			const pageInfo = f.pageCount ? ` (${f.pageCount}p)` : '';
			infoLines.push(`  ${chalk.dim('•')} ${chalk.cyan(f.name)} ${chalk.dim(`${format}, ${f.sizeDisplay}${pageInfo}`)}`);
		}
	} else {
		const f = resolvedFiles[0];
		const format = f.ext === '.pdf' ? 'PDF' : f.ext === '.md' || f.ext === '.markdown' ? 'Markdown' : 'Text';
		infoLines.push(`File:     ${chalk.cyan(f.name)}`);
		infoLines.push(`Format:   ${chalk.cyan(format)}`);
		infoLines.push(`Size:     ${chalk.cyan(f.sizeDisplay)}`);
		if (f.pageCount) infoLines.push(`Pages:    ${chalk.cyan(String(f.pageCount))}`);
	}

	const chunkSize = parseInt(options.chunkSize ?? '20', 10);
	const totalPages = resolvedFiles.reduce((sum, f) => sum + (f.pageCount ?? 0), 0);
	if (totalPages > chunkSize) {
		infoLines.push(`Chunks:   ${chalk.cyan(`${Math.ceil(totalPages / chunkSize)} × ${chunkSize} pages`)}`);
	}

	p.note(infoLines.join('\n'), isMulti ? 'Source Documents' : 'Source Document');

	// Generate spec ID
	const specId = `spec-${randomHex(4)}`;

	// Create spec directory and copy all files
	const specDir = join(cwd, '.forge', 'specs', specId);
	await ensureDir(specDir);

	const sourceFiles: string[] = [];
	for (let i = 0; i < resolvedFiles.length; i++) {
		const f = resolvedFiles[i];
		// Prefix with index to preserve order for multi-file
		const destName = isMulti ? `source-${i + 1}${f.ext}` : `source${f.ext}`;
		await copyFile(f.path, join(specDir, destName));
		sourceFiles.push(destName);
	}

	// If multiple text/markdown files, create a concatenated version for analysis
	let combinedPath: string | null = null;
	if (isMulti) {
		const parts: string[] = [];
		for (const f of resolvedFiles) {
			if (f.ext === '.pdf') {
				parts.push(`\n\n---\n# [PDF Document: ${f.name}]\n# (Read separately via PDF reader)\n---\n\n`);
			} else {
				const content = await readFile(f.path, 'utf-8');
				parts.push(`\n\n---\n# Document: ${f.name}\n---\n\n${content}`);
			}
		}
		combinedPath = join(specDir, 'combined.md');
		await writeText(combinedPath, parts.join(''));
	}

	p.log.success(`Copied ${resolvedFiles.length} file${isMulti ? 's' : ''} to ${chalk.dim(`.forge/specs/${specId}/`)}`);
	if (combinedPath) {
		p.log.success(`Combined document created at ${chalk.dim(`.forge/specs/${specId}/combined.md`)}`);
	}

	// Write spec metadata
	const meta = {
		spec_id: specId,
		source: {
			files: resolvedFiles.map((f, i) => ({
				original: f.name,
				stored: sourceFiles[i],
				format: f.ext.replace('.', ''),
				size_mb: parseFloat((f.sizeBytes / (1024 * 1024)).toFixed(2)),
				pages: f.pageCount,
			})),
			combined: combinedPath ? 'combined.md' : null,
			ingested_at: new Date().toISOString(),
		},
		status: 'pending-analysis',
		chunk_size: chunkSize,
	};
	await writeText(join(specDir, 'meta.json'), JSON.stringify(meta, null, 2));

	// Check if harness exists
	const harnessExists = await exists(join(cwd, 'forge.yaml'));

	if (!harnessExists) {
		const spinner = p.spinner();
		spinner.start('Analyzing spec with Claude Code...');

		let analysis: SpecAnalysis | null = null;
		// Use copied files in .forge/specs/ (not originals) so Claude can always access them
		const analysisTarget = combinedPath ?? join(specDir, sourceFiles[0]);
		const analysisExt = combinedPath ? '.md' : resolvedFiles[0].ext;
		const analysisPagesCount = combinedPath ? null : resolvedFiles[0].pageCount;

		try {
			analysis = await analyzeSpecWithClaude(analysisTarget, analysisExt, analysisPagesCount);
			spinner.stop('Spec analysis complete');
		} catch (err) {
			spinner.stop('Spec analysis failed');
			p.log.warn(chalk.yellow('Could not analyze spec automatically. You can configure manually.'));
			p.log.warn(chalk.dim(String(err)));
		}

		if (analysis) {
			const extractedLines = [
				`Project:      ${chalk.cyan(analysis.project_name)}`,
				`Description:  ${chalk.cyan(analysis.description)}`,
				`Type:         ${chalk.cyan(analysis.project_type)}`,
				`Language:     ${chalk.cyan(analysis.language)}`,
			];
			if (analysis.framework) extractedLines.push(`Framework:    ${chalk.cyan(analysis.framework)}`);
			if (analysis.modules.length > 0) extractedLines.push(`Modules:      ${chalk.cyan(analysis.modules.join(', '))}`);
			extractedLines.push(`Architecture: ${chalk.cyan(analysis.architecture)}`);
			if (analysis.sensitive_areas) extractedLines.push(`Sensitive:    ${chalk.cyan(analysis.sensitive_areas)}`);
			if (analysis.constraints.length > 0) extractedLines.push(`Constraints:  ${chalk.cyan(analysis.constraints.slice(0, 3).join('; '))}`);

			p.note(extractedLines.join('\n'), 'Extracted from spec');

			const confirmed = await p.confirm({ message: 'Does this look right?', initialValue: true });
			if (p.isCancel(confirmed)) { p.cancel('Cancelled.'); process.exit(0); }

			if (!confirmed) {
				const correctionsAnswer = await p.text({
					message: 'What needs to change?',
					placeholder: 'e.g. Use SvelteKit instead of Next.js, add billing as a module',
				});
				if (p.isCancel(correctionsAnswer)) { p.cancel('Cancelled.'); process.exit(0); }

				if (correctionsAnswer) {
					const spinner2 = p.spinner();
					spinner2.start('Re-analyzing with corrections...');
					try {
						analysis = await analyzeSpecWithClaude(analysisTarget, analysisExt, analysisPagesCount, correctionsAnswer as string);
						spinner2.stop('Updated analysis complete');
					} catch {
						spinner2.stop('Re-analysis failed, using original analysis');
					}
				}
			}

			await writeText(join(specDir, 'analysis.json'), JSON.stringify(analysis, null, 2));
			p.log.success('Spec analysis saved.');
		}
	}

	// Show next steps
	if (harnessExists) {
		p.log.step('Next step:');
		p.log.message(`  Open Claude Code → ${chalk.cyan(`/ingest ${specId}`)}`);
	} else {
		p.log.step('Next steps:');
		p.log.message(`  1. Run ${chalk.cyan('forge init')} to scaffold the harness`);
		p.log.message(`     ${chalk.dim('(use the spec analysis above to answer onboarding questions)')}`);
		p.log.message(`  2. Open Claude Code → ${chalk.cyan(`/ingest ${specId}`)}`);
	}

	p.outro(chalk.green('Spec ready for analysis.'));
}

/**
 * Called from `forge init --spec <file>` to analyze a spec and return
 * structured metadata that pre-fills the onboarding answers.
 */
export async function analyzeSpecForInit(specPath: string): Promise<SpecAnalysis | null> {
	const ext = extname(specPath).toLowerCase();
	let pageCount: number | null = null;
	if (ext === '.pdf') {
		pageCount = await detectPageCount(specPath);
	}
	return analyzeSpecWithClaude(specPath, ext, pageCount);
}

async function analyzeSpecWithClaude(
	specPath: string,
	ext: string,
	pageCount: number | null,
	corrections?: string,
): Promise<SpecAnalysis> {
	const prompt = buildAnalysisPrompt(specPath, ext, pageCount, corrections);

	// Write prompt to a temp file to avoid shell escaping issues
	const tmpPromptFile = join('/tmp', `forge-prompt-${randomHex(4)}.txt`);
	await writeText(tmpPromptFile, prompt);

	try {
		// Call claude as a subprocess, piping the prompt via stdin
		const result = await execaCommand(
			`cat "${tmpPromptFile}" | claude -p --output-format json`,
			{ timeout: 180000, shell: true },
		);

		const responseText = result.stdout;

		// claude -p --output-format json wraps output as:
		// {"type":"result","result":"<the actual content>", ...}
		let content: string;
		try {
			const wrapper = JSON.parse(responseText);
			content = wrapper.result ?? responseText;
		} catch {
			// If it's not valid JSON wrapper, use raw output
			content = responseText;
		}

		// Extract the JSON object from Claude's response
		// Try multiple patterns since Claude might wrap it in markdown code blocks
		let jsonStr: string | null = null;

		// Pattern 1: ```json ... ``` code block
		const codeBlockMatch = content.match(/```(?:json)?\s*(\{[\s\S]*?\})\s*```/);
		if (codeBlockMatch) {
			jsonStr = codeBlockMatch[1];
		}

		// Pattern 2: raw JSON object with project_name
		if (!jsonStr) {
			const rawMatch = content.match(/\{[\s\S]*?"project_name"[\s\S]*?\}/);
			if (rawMatch) {
				jsonStr = rawMatch[0];
			}
		}

		// Pattern 3: the entire content is JSON
		if (!jsonStr) {
			try {
				JSON.parse(content);
				jsonStr = content;
			} catch { /* not json */ }
		}

		if (!jsonStr) {
			throw new Error(`Could not find JSON in Claude response. Raw output:\n${content.slice(0, 500)}`);
		}

		const analysis: SpecAnalysis = JSON.parse(jsonStr);
		analysis.page_count = pageCount;

		// Validate required fields
		if (!analysis.project_name || !analysis.language) {
			throw new Error(`Incomplete analysis: missing project_name or language`);
		}

		return analysis;
	} finally {
		// Clean up temp file
		try {
			const { unlink } = await import('node:fs/promises');
			await unlink(tmpPromptFile);
		} catch { /* ignore */ }
	}
}

function buildAnalysisPrompt(
	specPath: string,
	ext: string,
	pageCount: number | null,
	corrections?: string,
): string {
	let readInstruction: string;
	if (ext === '.pdf') {
		const pagesToRead = pageCount && pageCount > 40 ? 40 : pageCount ?? 20;
		readInstruction = `Read the PDF at "${specPath}" (first ${pagesToRead} pages) to understand what this project is.`;
	} else {
		readInstruction = `Read the file at "${specPath}" to understand what this project is.`;
	}

	let correctionsClause = '';
	if (corrections) {
		correctionsClause = `\n\nThe user has provided these corrections to a previous analysis: "${corrections}". Apply these corrections to your analysis.`;
	}

	return `${readInstruction}

Extract the following as a JSON object. Output ONLY the JSON, no explanation, no markdown formatting:

{
  "project_name": "short project name",
  "description": "1-2 sentence description of what this project does",
  "language": "typescript | javascript | python | go",
  "framework": "next | sveltekit | fastapi | django | gin | null",
  "project_type": "web-app | api | cli | library | automation | fullstack",
  "modules": ["list", "of", "main", "modules"],
  "architecture": "monolith | client-server | microservices | static-site | library",
  "sensitive_areas": "description of sensitive data or security concerns, or empty string",
  "domain_rules": "key business rules agents must follow, or empty string",
  "constraints": ["list", "of", "hard", "constraints"]
}

Infer language and framework from the spec's technology requirements. If not specified, choose the best fit based on the project type. For medical/healthcare projects, note HIPAA requirements in sensitive_areas.${correctionsClause}`;
}

async function detectPageCount(pdfPath: string): Promise<number | null> {
	try {
		const result = await execaCommand(`mdls -name kMDItemNumberOfPages -raw "${pdfPath}"`, { shell: true });
		const count = parseInt(result.stdout.trim(), 10);
		if (!isNaN(count) && count > 0) return count;
	} catch { /* fall through */ }

	try {
		const result = await execaCommand(`pdfinfo "${pdfPath}" 2>/dev/null | grep "^Pages:" | awk '{print $2}'`, { shell: true });
		const count = parseInt(result.stdout.trim(), 10);
		if (!isNaN(count) && count > 0) return count;
	} catch { /* fall through */ }

	return null;
}

function randomHex(bytes: number): string {
	const array = new Uint8Array(bytes);
	crypto.getRandomValues(array);
	return Array.from(array, (b) => b.toString(16).padStart(2, '0')).join('');
}
