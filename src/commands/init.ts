import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { detect, type DetectedStack } from '../detect/index.js';
import { render } from '../render/engine.js';
import { exists, readText, writeText, ensureDir } from '../utils/fs.js';
import { hashContent, writeHashes, type HashManifest } from '../utils/hash.js';
import { isGitRepo, getMainBranch } from '../utils/git.js';
import { resolveTemplatePath } from '../utils/fs.js';

interface InitOptions {
	preset?: string;
	force?: boolean;
	yes?: boolean;
}

const AVAILABLE_PRESETS = ['sveltekit-ts', 'react-next-ts', 'python-fastapi', 'go'] as const;

export async function init(options: InitOptions): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge v0.1.0') + chalk.dim(' — Agent Harness for Claude Code'));

	// Check for existing harness
	if (!options.force && (await exists(join(cwd, 'forge.yaml')))) {
		const shouldOverwrite = await p.confirm({
			message: 'A forge.yaml already exists. Overwrite?',
			initialValue: false,
		});
		if (p.isCancel(shouldOverwrite) || !shouldOverwrite) {
			p.cancel('Init cancelled. Use --force to overwrite.');
			process.exit(0);
		}
	}

	// Phase 1: Detect
	const spinner = p.spinner();
	spinner.start('Scanning project...');
	const detected = await detect(cwd);
	spinner.stop('Scan complete');

	displayDetected(detected, cwd);

	// Phase 2: Confirm + ask questions
	const answers = await askQuestions(detected, options);

	// Phase 3: Generate
	spinner.start('Generating harness...');
	const files = await generateHarness(cwd, detected, answers);
	spinner.stop(`${files.length} files created`);

	// Phase 4: Display results
	displayResults(files, answers);

	p.outro(chalk.green('Harness ready!') + chalk.dim(' Run /deliver in Claude Code to start.'));
}

function displayDetected(detected: DetectedStack, cwd: string): void {
	const lines: string[] = [];

	if (detected.language !== 'unknown') {
		lines.push(`Language:    ${chalk.cyan(detected.language)}`);
	}
	if (detected.framework) {
		lines.push(`Framework:   ${chalk.cyan(detected.framework)}`);
	}
	if (detected.testRunner) {
		lines.push(`Testing:     ${chalk.cyan(detected.testRunner.name)}`);
	}
	if (detected.linter) {
		lines.push(`Linting:     ${chalk.cyan(detected.linter.name)}`);
	}
	if (detected.typeChecker) {
		lines.push(`Type check:  ${chalk.cyan(detected.typeChecker.name)}`);
	}
	if (detected.features.git) {
		lines.push(`VCS:         ${chalk.cyan('Git')}`);
	}

	if (lines.length > 0) {
		p.note(lines.join('\n'), 'Detected');
	} else {
		p.log.warn('Could not auto-detect project stack. You will need to select a preset manually.');
	}
}

interface Answers {
	preset: string;
	commands: {
		typecheck: string;
		lint: string;
		test: string;
		format: string;
		dev: string;
	};
	autoPr: boolean;
	projectName: string;
}

async function askQuestions(detected: DetectedStack, options: InitOptions): Promise<Answers> {
	// Preset selection
	let preset = options.preset;
	if (!preset) {
		const presetOptions = AVAILABLE_PRESETS.map((p) => ({
			value: p,
			label: p + (p === detected.preset ? chalk.dim(' (recommended — matches your stack)') : ''),
		}));

		// Put detected preset first
		if (detected.preset) {
			const idx = presetOptions.findIndex((o) => o.value === detected.preset);
			if (idx > 0) {
				const [match] = presetOptions.splice(idx, 1);
				presetOptions.unshift(match);
			}
		}

		const selected = options.yes
			? detected.preset ?? 'sveltekit-ts'
			: await p.select({
					message: 'Select preset:',
					options: presetOptions,
				});

		if (p.isCancel(selected)) {
			p.cancel('Init cancelled.');
			process.exit(0);
		}
		preset = selected as string;
	}

	// Commands — show detected defaults, let user override
	const typecheck = detected.typeChecker?.command ?? 'npm run check';
	const lint = detected.linter?.command ?? 'npm run lint';
	const test = detected.testRunner?.command ?? 'npx vitest run';
	const format = detected.formatter?.command ?? '';
	const dev = 'npm run dev';

	if (!options.yes) {
		p.note(
			[
				`Typecheck: ${chalk.cyan(typecheck)}`,
				`Lint:      ${chalk.cyan(lint)}`,
				`Test:      ${chalk.cyan(test)}`,
				format ? `Format:    ${chalk.cyan(format)}` : null,
			]
				.filter(Boolean)
				.join('\n'),
			'Verification commands (edit in forge.yaml later)',
		);
	}

	// Auto-PR
	const autoPr = options.yes
		? true
		: await p.confirm({
				message: 'Auto-create PRs on delivery?',
				initialValue: true,
			});

	if (p.isCancel(autoPr)) {
		p.cancel('Init cancelled.');
		process.exit(0);
	}

	// Project name — infer from directory
	const projectName = process.cwd().split('/').pop() ?? 'my-app';

	return {
		preset,
		commands: { typecheck, lint, test, format, dev },
		autoPr: autoPr as boolean,
		projectName,
	};
}

interface GeneratedFile {
	relativePath: string;
	content: string;
}

async function generateHarness(
	cwd: string,
	detected: DetectedStack,
	answers: Answers,
): Promise<GeneratedFile[]> {
	const ctx = buildTemplateContext(detected, answers);
	const files: GeneratedFile[] = [];
	const hashes: HashManifest = { version: '0.1.0', files: {} };

	// Determine which agents to generate based on preset
	const isFrontendPreset = ['sveltekit-ts', 'react-next-ts', 'vue-nuxt-ts'].includes(answers.preset);

	// Define all files to generate: [templatePath, outputPath]
	const fileMap: [string, string][] = [
		['core/forge.yaml.hbs', 'forge.yaml'],
		['core/CLAUDE.md.hbs', 'CLAUDE.md'],
		['core/settings.json.hbs', '.claude/settings.json'],
		['core/skill-deliver.md.hbs', '.claude/skills/deliver/SKILL.md'],
		['core/pipeline/orchestrator.sh.hbs', '.forge/pipeline/orchestrator.sh'],
		['core/pipeline/intake.sh.hbs', '.forge/pipeline/intake.sh'],
		['core/pipeline/classify.sh.hbs', '.forge/pipeline/classify.sh'],
		['core/pipeline/decompose.md.hbs', '.forge/pipeline/decompose.md'],
		['core/pipeline/execute.md.hbs', '.forge/pipeline/execute.md'],
		['core/pipeline/verify.sh.hbs', '.forge/pipeline/verify.sh'],
		['core/pipeline/deliver.sh.hbs', '.forge/pipeline/deliver.sh'],
		['core/pipeline/bead-state.sh.hbs', '.forge/pipeline/bead-state.sh'],
		['core/agents/architect.md.hbs', '.forge/agents/architect.md'],
		['core/agents/quality.md.hbs', '.forge/agents/quality.md'],
		['core/agents/security.md.hbs', '.forge/agents/security.md'],
		['core/context/project.md.hbs', '.forge/context/project.md'],
		['core/hooks/pre-edit.sh.hbs', '.forge/hooks/pre-edit.sh'],
		['core/hooks/post-edit.sh.hbs', '.forge/hooks/post-edit.sh'],
		['core/hooks/session-start.sh.hbs', '.forge/hooks/session-start.sh'],
		['core/beads/config.yaml.hbs', '.forge/beads/config.yaml'],
	];

	// Add frontend agent if applicable
	if (isFrontendPreset) {
		fileMap.push(['core/agents/frontend.md.hbs', '.forge/agents/frontend.md']);
	}
	fileMap.push(['core/agents/backend.md.hbs', '.forge/agents/backend.md']);

	// Add stack-specific context from preset
	const presetContextPath = `presets/${answers.preset}/stack.md.hbs`;
	fileMap.push([presetContextPath, '.forge/context/stack.md']);

	for (const [templateRel, outputRel] of fileMap) {
		const templatePath = resolveTemplatePath(templateRel);
		let content: string;

		try {
			const template = await readText(templatePath);
			content = render(template, ctx);
		} catch {
			// Template doesn't exist yet — write a placeholder
			content = `# ${outputRel}\n\n> Template not yet created: ${templateRel}\n> This file will be populated in a future phase.\n`;
		}

		const outputPath = join(cwd, outputRel);
		await writeText(outputPath, content);
		files.push({ relativePath: outputRel, content });
		hashes.files[outputRel] = hashContent(content);

		// Make shell scripts executable
		if (outputRel.endsWith('.sh')) {
			const { chmod } = await import('node:fs/promises');
			await chmod(outputPath, 0o755);
		}
	}

	// Create empty directories
	await ensureDir(join(cwd, '.forge', 'beads', 'active'));
	await ensureDir(join(cwd, '.forge', 'beads', 'archive'));
	await ensureDir(join(cwd, '.forge', 'addons'));
	await ensureDir(join(cwd, '.forge', 'state'));

	// Write .gitkeep for state
	await writeText(join(cwd, '.forge', 'state', '.gitkeep'), '');

	// Write hash manifest
	await writeHashes(cwd, hashes);

	return files;
}

function buildTemplateContext(detected: DetectedStack, answers: Answers): Record<string, unknown> {
	const isFrontendPreset = ['sveltekit-ts', 'react-next-ts', 'vue-nuxt-ts'].includes(answers.preset);

	const agents = ['architect', 'quality', 'security', 'backend'];
	if (isFrontendPreset) agents.splice(3, 0, 'frontend');

	return {
		project: {
			name: answers.projectName,
			preset: answers.preset,
		},
		commands: answers.commands,
		agents,
		has_frontend: isFrontendPreset,
		has_format: Boolean(answers.commands.format),
		auto_pr: answers.autoPr,
		detected: {
			language: detected.language,
			framework: detected.framework,
			features: detected.features,
		},
		// Preset-specific context
		preset: answers.preset,
		is_sveltekit: answers.preset === 'sveltekit-ts',
		is_nextjs: answers.preset === 'react-next-ts',
		is_fastapi: answers.preset === 'python-fastapi',
		is_go: answers.preset === 'go',
	};
}

function displayResults(files: GeneratedFile[], answers: Answers): void {
	const lines = files.map(
		(f) => `  ${chalk.green('✓')} ${f.relativePath}`,
	);

	p.note(lines.join('\n'), `Generated (${files.length} files)`);

	p.log.step('Next steps:');
	p.log.message(`  1. Edit ${chalk.cyan('.forge/context/project.md')} — add your architecture notes`);
	p.log.message(`  2. ${chalk.dim('git add forge.yaml CLAUDE.md .claude .forge')}`);
	p.log.message(`  3. ${chalk.dim('git commit -m "forge: initialize agent harness"')}`);
	p.log.message(`  4. Open Claude Code → ${chalk.cyan('/deliver "your first task"')}`);
	p.log.message('');
	p.log.message(`  ${chalk.dim('Optional:')}`);
	p.log.message(`    ${chalk.dim('forge add browser-testing')}    — Playwright visual QA`);
	p.log.message(`    ${chalk.dim('forge add compliance-hipaa')}   — HIPAA security checks`);
	p.log.message(`    ${chalk.dim('forge doctor')}                 — Verify harness health`);
}
