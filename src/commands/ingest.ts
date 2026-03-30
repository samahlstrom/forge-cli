import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join, resolve, extname, basename } from 'node:path';
import { exists, readText, writeText, ensureDir } from '../utils/fs.js';
import { execaCommand } from 'execa';
import { copyFile, stat } from 'node:fs/promises';

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

export async function ingest(file: string, options: IngestOptions): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge') + chalk.dim(' — Spec Ingestion'));

	// Resolve and validate spec file
	const specPath = resolve(file);
	if (!(await exists(specPath))) {
		p.cancel(`File not found: ${specPath}`);
		process.exit(1);
	}

	const ext = extname(specPath).toLowerCase();
	const supportedFormats = ['.pdf', '.md', '.txt', '.markdown'];
	if (!supportedFormats.includes(ext)) {
		p.cancel(`Unsupported format: ${ext}. Supported: ${supportedFormats.join(', ')}`);
		process.exit(1);
	}

	// Get file info
	const fileStats = await stat(specPath);
	const fileSizeMB = (fileStats.size / (1024 * 1024)).toFixed(1);
	const format = ext === '.pdf' ? 'PDF' : ext === '.md' || ext === '.markdown' ? 'Markdown' : 'Text';

	// Detect page count for PDFs
	let pageCount: number | null = null;
	if (ext === '.pdf') {
		pageCount = await detectPageCount(specPath);
	}

	const chunkSize = parseInt(options.chunkSize ?? '20', 10);
	const chunkCount = pageCount ? Math.ceil(pageCount / chunkSize) : null;

	const infoLines = [
		`File:     ${chalk.cyan(basename(specPath))}`,
		`Format:   ${chalk.cyan(format)}`,
		`Size:     ${chalk.cyan(fileSizeMB + ' MB')}`,
	];
	if (pageCount) infoLines.push(`Pages:    ${chalk.cyan(String(pageCount))}`);
	if (chunkCount && chunkCount > 1) infoLines.push(`Chunks:   ${chalk.cyan(`${chunkCount} × ${chunkSize} pages`)}`);

	p.note(infoLines.join('\n'), 'Source Document');

	// Generate spec ID
	const specId = `spec-${randomHex(4)}`;

	// Create spec directory and copy file
	const specDir = join(cwd, '.forge', 'specs', specId);
	await ensureDir(specDir);
	const destFile = join(specDir, `source${ext}`);
	await copyFile(specPath, destFile);

	p.log.success(`Copied to ${chalk.dim(`.forge/specs/${specId}/source${ext}`)}`);

	// Write spec metadata
	const meta = {
		spec_id: specId,
		source: {
			file: basename(specPath),
			format: ext.replace('.', ''),
			size_mb: parseFloat(fileSizeMB),
			pages: pageCount,
			ingested_at: new Date().toISOString(),
		},
		status: 'pending-analysis',
		chunk_size: chunkSize,
	};
	await writeText(join(specDir, 'meta.json'), JSON.stringify(meta, null, 2));

	// Check if harness exists — if not, this is a --spec init
	const harnessExists = await exists(join(cwd, 'forge.yaml'));

	if (!harnessExists) {
		// This was called via `forge init --spec` — analyze spec to extract project metadata
		const spinner = p.spinner();
		spinner.start('Analyzing spec with Claude Code...');

		let analysis: SpecAnalysis | null = null;
		try {
			analysis = await analyzeSpecWithClaude(specPath, ext, pageCount);
			spinner.stop('Spec analysis complete');
		} catch (err) {
			spinner.stop('Spec analysis failed');
			p.log.warn(chalk.yellow('Could not analyze spec automatically. You can configure manually.'));
			p.log.warn(chalk.dim(String(err)));
		}

		if (analysis) {
			// Show extracted metadata for confirmation
			const extractedLines = [
				`Project:      ${chalk.cyan(analysis.project_name)}`,
				`Description:  ${chalk.cyan(analysis.description)}`,
				`Type:         ${chalk.cyan(analysis.project_type)}`,
				`Language:     ${chalk.cyan(analysis.language)}`,
			];
			if (analysis.framework) {
				extractedLines.push(`Framework:    ${chalk.cyan(analysis.framework)}`);
			}
			if (analysis.modules.length > 0) {
				extractedLines.push(`Modules:      ${chalk.cyan(analysis.modules.join(', '))}`);
			}
			extractedLines.push(`Architecture: ${chalk.cyan(analysis.architecture)}`);
			if (analysis.sensitive_areas) {
				extractedLines.push(`Sensitive:    ${chalk.cyan(analysis.sensitive_areas)}`);
			}
			if (analysis.constraints.length > 0) {
				extractedLines.push(`Constraints:  ${chalk.cyan(analysis.constraints.slice(0, 3).join('; '))}`);
			}

			p.note(extractedLines.join('\n'), 'Extracted from spec');

			const confirmed = await p.confirm({
				message: 'Does this look right?',
				initialValue: true,
			});
			if (p.isCancel(confirmed)) { p.cancel('Cancelled.'); process.exit(0); }

			let corrections = '';
			if (!confirmed) {
				const correctionsAnswer = await p.text({
					message: 'What needs to change?',
					placeholder: 'e.g. Use SvelteKit instead of Next.js, add billing as a module',
				});
				if (p.isCancel(correctionsAnswer)) { p.cancel('Cancelled.'); process.exit(0); }
				corrections = correctionsAnswer as string;

				// Re-analyze with corrections
				if (corrections) {
					const spinner2 = p.spinner();
					spinner2.start('Re-analyzing with corrections...');
					try {
						analysis = await analyzeSpecWithClaude(specPath, ext, pageCount, corrections);
						spinner2.stop('Updated analysis complete');
					} catch {
						spinner2.stop('Re-analysis failed, using original analysis');
					}
				}
			}

			// Write the analysis to the spec directory for the /ingest skill to use
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

	// Call claude as a subprocess
	const result = await execaCommand(
		`claude -p "${escapeShell(prompt)}" --output-format json`,
		{ timeout: 120000, shell: true },
	);

	// Parse the response — claude outputs JSON with a "result" field
	let responseText = result.stdout;

	// Try to extract JSON from the response
	const jsonMatch = responseText.match(/\{[\s\S]*"project_name"[\s\S]*\}/);
	if (!jsonMatch) {
		throw new Error('Could not parse spec analysis from Claude response');
	}

	const analysis: SpecAnalysis = JSON.parse(jsonMatch[0]);
	analysis.page_count = pageCount;

	return analysis;
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

Extract the following as a JSON object (and ONLY a JSON object, no other text):

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
	// Try macOS mdls first
	try {
		const result = await execaCommand(`mdls -name kMDItemNumberOfPages -raw "${pdfPath}"`, { shell: true });
		const count = parseInt(result.stdout.trim(), 10);
		if (!isNaN(count) && count > 0) return count;
	} catch { /* fall through */ }

	// Try pdfinfo (poppler-utils)
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

function escapeShell(str: string): string {
	return str.replace(/"/g, '\\"').replace(/\$/g, '\\$').replace(/`/g, '\\`');
}
