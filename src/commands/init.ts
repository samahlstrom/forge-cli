import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join, resolve, extname, basename } from 'node:path';
import { detect, type DetectedStack } from '../detect/index.js';
import { render } from '../render/engine.js';
import { exists, readText, writeText, ensureDir } from '../utils/fs.js';
import { hashContent, writeHashes, type HashManifest } from '../utils/hash.js';
import { isGitRepo, getMainBranch } from '../utils/git.js';
import { resolveTemplatePath } from '../utils/fs.js';
import { analyzeSpecForInit } from './ingest.js';
import { copyFile } from 'node:fs/promises';

export interface InitOptions {
	preset?: string;
	force?: boolean;
	yes?: boolean;
	spec?: string;
	/** Pre-computed spec analysis from forge ingest — skips re-analysis */
	specAnalysis?: SpecAnalysis | null;
	/** Pre-existing spec ID from forge ingest */
	specId?: string;
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

const AVAILABLE_PRESETS = ['sveltekit-ts', 'react-next-ts', 'python-fastapi', 'go'] as const;

// Maps user-friendly language + framework choices to presets and default commands
const LANGUAGE_OPTIONS = [
	{ value: 'typescript', label: 'TypeScript' },
	{ value: 'javascript', label: 'JavaScript' },
	{ value: 'python', label: 'Python' },
	{ value: 'go', label: 'Go' },
] as const;

const FRAMEWORK_OPTIONS: Record<string, { value: string; label: string; preset: string }[]> = {
	typescript: [
		{ value: 'next', label: 'Next.js', preset: 'react-next-ts' },
		{ value: 'sveltekit', label: 'SvelteKit', preset: 'sveltekit-ts' },
		{ value: 'other', label: 'Other / None', preset: 'react-next-ts' },
	],
	javascript: [
		{ value: 'next', label: 'Next.js', preset: 'react-next-ts' },
		{ value: 'sveltekit', label: 'SvelteKit', preset: 'sveltekit-ts' },
		{ value: 'other', label: 'Other / None', preset: 'react-next-ts' },
	],
	python: [
		{ value: 'fastapi', label: 'FastAPI', preset: 'python-fastapi' },
		{ value: 'django', label: 'Django', preset: 'python-fastapi' },
		{ value: 'flask', label: 'Flask', preset: 'python-fastapi' },
		{ value: 'other', label: 'Other / None', preset: 'python-fastapi' },
	],
	go: [
		{ value: 'gin', label: 'Gin', preset: 'go' },
		{ value: 'chi', label: 'Chi', preset: 'go' },
		{ value: 'fiber', label: 'Fiber', preset: 'go' },
		{ value: 'other', label: 'Other / None', preset: 'go' },
	],
};

const DEFAULT_COMMANDS: Record<string, { typecheck: string; lint: string; test: string; format: string; dev: string }> = {
	typescript: { typecheck: 'npx tsc --noEmit', lint: 'npm run lint', test: 'npx vitest run', format: 'npx prettier --write .', dev: 'npm run dev' },
	javascript: { typecheck: 'echo "no typecheck"', lint: 'npm run lint', test: 'npx vitest run', format: 'npx prettier --write .', dev: 'npm run dev' },
	python: { typecheck: 'mypy .', lint: 'ruff check .', test: 'pytest', format: 'ruff format .', dev: 'uvicorn app.main:app --reload' },
	go: { typecheck: 'go vet ./...', lint: 'golangci-lint run', test: 'go test ./...', format: 'gofmt -w .', dev: 'go run .' },
};

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

	// Phase 2: Spec analysis
	let specAnalysis: SpecAnalysis | null = options.specAnalysis ?? null;
	let specId: string | null = options.specId ?? null;

	if (options.spec && !specAnalysis) {
		const specPath = resolve(options.spec);
		if (!(await exists(specPath))) {
			p.cancel(`Spec file not found: ${specPath}`);
			process.exit(1);
		}

		spinner.start('Analyzing spec with Claude Code...');
		try {
			specAnalysis = await analyzeSpecForInit(specPath);
			spinner.stop('Spec analysis complete');
		} catch (err) {
			spinner.stop('Spec analysis failed');
			p.log.warn(chalk.yellow(`Could not analyze spec: ${err}`));
			p.log.warn(chalk.dim('Falling back to manual onboarding.'));
		}

		if (specAnalysis) {
			const extractedLines = [
				`Project:      ${chalk.cyan(specAnalysis.project_name)}`,
				`Description:  ${chalk.cyan(specAnalysis.description)}`,
				`Type:         ${chalk.cyan(specAnalysis.project_type)}`,
				`Language:     ${chalk.cyan(specAnalysis.language)}`,
			];
			if (specAnalysis.framework) extractedLines.push(`Framework:    ${chalk.cyan(specAnalysis.framework)}`);
			if (specAnalysis.modules.length > 0) extractedLines.push(`Modules:      ${chalk.cyan(specAnalysis.modules.join(', '))}`);
			extractedLines.push(`Architecture: ${chalk.cyan(specAnalysis.architecture)}`);
			if (specAnalysis.sensitive_areas) extractedLines.push(`Sensitive:    ${chalk.cyan(specAnalysis.sensitive_areas)}`);
			if (specAnalysis.constraints.length > 0) extractedLines.push(`Constraints:  ${chalk.cyan(specAnalysis.constraints.slice(0, 3).join('; '))}`);

			p.note(extractedLines.join('\n'), 'Extracted from spec');

			const confirmed = await p.confirm({ message: 'Does this look right?', initialValue: true });
			if (p.isCancel(confirmed)) { p.cancel('Cancelled.'); process.exit(0); }

			if (!confirmed) {
				const corrections = await p.text({
					message: 'What needs to change?',
					placeholder: 'e.g. Use SvelteKit instead of Next.js',
				});
				if (p.isCancel(corrections)) { p.cancel('Cancelled.'); process.exit(0); }

				if (corrections) {
					spinner.start('Re-analyzing with corrections...');
					try {
						specAnalysis = await analyzeSpecForInit(specPath);
						spinner.stop('Updated');
					} catch {
						spinner.stop('Re-analysis failed, using original');
					}
				}
			}
		}

		// Copy spec into .forge/specs/ (skip if already done by forge ingest)
		if (!specId) {
			const randomHex = (n: number) => Array.from(crypto.getRandomValues(new Uint8Array(n)), b => b.toString(16).padStart(2, '0')).join('');
			specId = `spec-${randomHex(4)}`;
			const specDir = join(cwd, '.forge', 'specs', specId);
			await ensureDir(specDir);
			await copyFile(specPath, join(specDir, `source${extname(specPath)}`));

			if (specAnalysis) {
				await writeText(join(specDir, 'analysis.json'), JSON.stringify(specAnalysis, null, 2));
			}

			await writeText(join(specDir, 'meta.json'), JSON.stringify({
				spec_id: specId,
				source: { file: basename(specPath), format: extname(specPath).replace('.', '') },
				status: 'pending-analysis',
				ingested_at: new Date().toISOString(),
			}, null, 2));
		}
	}

	// Phase 3: Confirm + ask questions (pre-filled from spec if available)
	const answers = await askQuestions(detected, options, specAnalysis);

	// Phase 4: Generate
	spinner.start('Generating harness...');
	const files = await generateHarness(cwd, detected, answers);
	spinner.stop(`${files.length} files created`);

	// Phase 5: Display results
	displayResults(files, answers, specId);

	p.log.warn(chalk.yellow('If you ran this inside Claude Code, restart the session so it picks up the new settings, skills, and hooks.'));
	if (specId) {
		p.outro(chalk.green('Harness ready!') + chalk.dim(` Run /ingest ${specId} in Claude Code to start.`));
	} else {
		p.outro(chalk.green('Harness ready!') + chalk.dim(' Run /deliver in Claude Code to start.'));
	}
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
	projectDescription: string;
	projectType: string;
	keyModules: string[];
	architectureStyle: string;
	sensitivePaths: string;
	domainRules: string;
}

async function askQuestions(
	detected: DetectedStack,
	options: InitOptions,
	specAnalysis?: Awaited<ReturnType<typeof analyzeSpecForInit>> | null,
): Promise<Answers> {
	const projectName = specAnalysis?.project_name ?? process.cwd().split('/').pop() ?? 'my-app';
	const nothingDetected = detected.language === 'unknown';

	// --- Stack selection ---
	let preset = options.preset ?? null;
	let chosenLanguage: string = detected.language !== 'unknown' ? detected.language : 'typescript';
	let commands = { typecheck: '', lint: '', test: '', format: '', dev: '' };

	// If spec analysis provided language/framework, use those
	if (specAnalysis && !preset) {
		chosenLanguage = specAnalysis.language || 'typescript';
		// Map spec framework to preset
		const frameworkMap: Record<string, string> = {
			next: 'react-next-ts', sveltekit: 'sveltekit-ts',
			fastapi: 'python-fastapi', django: 'python-fastapi', flask: 'python-fastapi',
			gin: 'go', chi: 'go', fiber: 'go',
		};
		if (specAnalysis.framework && frameworkMap[specAnalysis.framework]) {
			preset = frameworkMap[specAnalysis.framework];
		} else {
			// Fall back to language default
			const langPresets: Record<string, string> = {
				typescript: 'react-next-ts', javascript: 'react-next-ts',
				python: 'python-fastapi', go: 'go',
			};
			preset = langPresets[chosenLanguage] ?? 'react-next-ts';
		}
	} else if (!preset && !options.yes) {
		if (nothingDetected) {
			// Empty repo — ask what they want to build
			p.log.step('No existing code detected — let\'s set up your stack.');

			const langAnswer = await p.select({
				message: 'What language will you use?',
				options: LANGUAGE_OPTIONS.map((o) => ({ value: o.value, label: o.label })),
			});
			if (p.isCancel(langAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
			chosenLanguage = langAnswer as string;

			const frameworks = FRAMEWORK_OPTIONS[chosenLanguage] ?? [];
			if (frameworks.length > 0) {
				const fwAnswer = await p.select({
					message: 'What framework?',
					options: frameworks.map((o) => ({ value: o.value, label: o.label })),
				});
				if (p.isCancel(fwAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
				const chosen = frameworks.find((f) => f.value === fwAnswer);
				preset = chosen?.preset ?? frameworks[0].preset;
			}
		} else {
			// Existing code detected — confirm or let them change
			if (detected.preset) {
				const confirmPreset = await p.confirm({
					message: `Detected ${chalk.cyan(detected.preset)} — use this preset?`,
					initialValue: true,
				});
				if (p.isCancel(confirmPreset)) { p.cancel('Init cancelled.'); process.exit(0); }

				if (confirmPreset) {
					preset = detected.preset;
				}
			}

			if (!preset) {
				const presetOptions = AVAILABLE_PRESETS.map((pr) => ({
					value: pr,
					label: pr + (pr === detected.preset ? chalk.dim(' (detected)') : ''),
				}));
				if (detected.preset) {
					const idx = presetOptions.findIndex((o) => o.value === detected.preset);
					if (idx > 0) {
						const [match] = presetOptions.splice(idx, 1);
						presetOptions.unshift(match);
					}
				}
				const selected = await p.select({ message: 'Select preset:', options: presetOptions });
				if (p.isCancel(selected)) { p.cancel('Init cancelled.'); process.exit(0); }
				preset = selected as string;
			}
		}
	}

	// Fallback for --yes or if still null
	if (!preset) {
		preset = detected.preset ?? 'react-next-ts';
	}

	// --- Commands ---
	// Use detected commands if available, otherwise fall back to language defaults
	const langDefaults = DEFAULT_COMMANDS[chosenLanguage] ?? DEFAULT_COMMANDS.typescript;
	commands = {
		typecheck: detected.typeChecker?.command ?? langDefaults.typecheck,
		lint: detected.linter?.command ?? langDefaults.lint,
		test: detected.testRunner?.command ?? langDefaults.test,
		format: detected.formatter?.command ?? langDefaults.format,
		dev: langDefaults.dev,
	};

	if (!options.yes) {
		p.note(
			[
				`Typecheck: ${chalk.cyan(commands.typecheck)}`,
				`Lint:      ${chalk.cyan(commands.lint)}`,
				`Test:      ${chalk.cyan(commands.test)}`,
				commands.format ? `Format:    ${chalk.cyan(commands.format)}` : null,
			]
				.filter(Boolean)
				.join('\n'),
			'Verification commands (edit in forge.yaml later)',
		);
	}

	// --- Auto-PR ---
	const autoPr = options.yes
		? true
		: await p.confirm({
				message: 'Auto-create PRs on delivery?',
				initialValue: true,
			});
	if (p.isCancel(autoPr)) { p.cancel('Init cancelled.'); process.exit(0); }

	// --- Onboarding ---
	// If spec analysis is available, use it to pre-fill; otherwise ask interactively
	let projectDescription = specAnalysis?.description ?? '';
	let projectType = specAnalysis?.project_type ?? 'web-app';
	let keyModules: string[] = specAnalysis?.modules ?? [];
	let architectureStyle = specAnalysis?.architecture ?? 'monolith';
	let sensitivePaths = specAnalysis?.sensitive_areas ?? '';
	let domainRules = specAnalysis?.domain_rules ?? '';

	if (!options.yes && !specAnalysis) {
		// No spec — ask everything interactively
		p.log.step('Tell us about your project so agents understand what they\'re working on.');

		const descAnswer = await p.text({
			message: 'What are you building?',
			placeholder: 'e.g. A SaaS platform for restaurant inventory management',
			validate: (val) => {
				if (!val.trim()) return 'A short description helps agents write better code.';
			},
		});
		if (p.isCancel(descAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
		projectDescription = descAnswer as string;

		const typeAnswer = await p.select({
			message: 'What kind of project is this?',
			options: [
				{ value: 'web-app', label: 'Web application — frontend + backend' },
				{ value: 'api', label: 'API / Backend service — no frontend' },
				{ value: 'cli', label: 'CLI tool — command-line interface' },
				{ value: 'library', label: 'Library / Package — consumed by other projects' },
				{ value: 'automation', label: 'Automation / Scripts — GitHub Actions, bots, pipelines' },
				{ value: 'fullstack', label: 'Full-stack monorepo — multiple apps in one repo' },
			],
		});
		if (p.isCancel(typeAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
		projectType = typeAnswer as string;

		const modulesAnswer = await p.text({
			message: 'What are the main features or modules?',
			placeholder: 'e.g. auth, dashboard, inventory, notifications',
		});
		if (p.isCancel(modulesAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
		keyModules = (modulesAnswer as string).split(',').map((s) => s.trim()).filter(Boolean);

		const archAnswer = await p.select({
			message: 'How is the app structured?',
			options: [
				{ value: 'monolith', label: 'Monolith — single deployable unit' },
				{ value: 'client-server', label: 'Client + Server — separate frontend and backend' },
				{ value: 'microservices', label: 'Microservices — multiple independent services' },
				{ value: 'static-site', label: 'Static site — pre-rendered or JAMstack' },
				{ value: 'library', label: 'Library / Package — consumed by other projects' },
			],
		});
		if (p.isCancel(archAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
		architectureStyle = archAnswer as string;

		const sensitiveAnswer = await p.text({
			message: 'Any sensitive areas? (leave blank to skip)',
			placeholder: 'e.g. src/auth/ handles tokens, src/payments/ has Stripe integration',
		});
		if (p.isCancel(sensitiveAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
		sensitivePaths = (sensitiveAnswer as string) || '';

		const domainAnswer = await p.text({
			message: 'Any domain-specific rules agents should know? (leave blank to skip)',
			placeholder: 'e.g. All prices stored in cents. Users always belong to exactly one org.',
		});
		if (p.isCancel(domainAnswer)) { p.cancel('Init cancelled.'); process.exit(0); }
		domainRules = (domainAnswer as string) || '';
	}

	return {
		preset,
		commands,
		autoPr: autoPr as boolean,
		projectName,
		projectDescription,
		projectType,
		keyModules,
		architectureStyle,
		sensitivePaths,
		domainRules,
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

	// Determine which agents to generate based on project type
	const projectType = answers.projectType;
	const needsFrontend = ['web-app', 'fullstack'].includes(projectType);
	const needsBackend = ['web-app', 'api', 'fullstack', 'microservices'].includes(projectType);

	// Define all files to generate: [templatePath, outputPath]
	const fileMap: [string, string][] = [
		['core/forge.yaml.hbs', 'forge.yaml'],
		['core/CLAUDE.md.hbs', 'CLAUDE.md'],
		['core/settings.json.hbs', '.claude/settings.json'],
		['core/skill-deliver.md.hbs', '.claude/skills/deliver/SKILL.md'],
		['core/skill-creator.md.hbs', '.claude/skills/skill-creator/SKILL.md'],
		['core/skill-ingest.md.hbs', '.claude/skills/ingest/SKILL.md'],
		['core/pipeline/orchestrator.sh.hbs', '.forge/pipeline/orchestrator.sh'],
		['core/pipeline/intake.sh.hbs', '.forge/pipeline/intake.sh'],
		['core/pipeline/classify.sh.hbs', '.forge/pipeline/classify.sh'],
		['core/pipeline/decompose.md.hbs', '.forge/pipeline/decompose.md'],
		['core/pipeline/execute.md.hbs', '.forge/pipeline/execute.md'],
		['core/pipeline/verify.sh.hbs', '.forge/pipeline/verify.sh'],
		['core/pipeline/deliver.sh.hbs', '.forge/pipeline/deliver.sh'],
		['core/agents/architect.md.hbs', '.forge/agents/architect.md'],
		['core/agents/quality.md.hbs', '.forge/agents/quality.md'],
		['core/agents/security.md.hbs', '.forge/agents/security.md'],
		['core/context/project.md.hbs', '.forge/context/project.md'],
		['core/hooks/pre-edit.sh.hbs', '.forge/hooks/pre-edit.sh'],
		['core/hooks/post-edit.sh.hbs', '.forge/hooks/post-edit.sh'],
		['core/hooks/session-start.sh.hbs', '.forge/hooks/session-start.sh'],
	];

	// Add agents based on project type
	if (needsFrontend) {
		fileMap.push(['core/agents/frontend.md.hbs', '.forge/agents/frontend.md']);
	}
	if (needsBackend) {
		fileMap.push(['core/agents/backend.md.hbs', '.forge/agents/backend.md']);
	}

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
	await ensureDir(join(cwd, '.forge', 'addons'));
	await ensureDir(join(cwd, '.forge', 'state'));
	await ensureDir(join(cwd, '.forge', 'pipeline', 'runs'));

	// Write .gitkeep for state
	await writeText(join(cwd, '.forge', 'state', '.gitkeep'), '');

	// Write hash manifest
	await writeHashes(cwd, hashes);

	// Initialize bd (beads) for task tracking
	try {
		const { execaCommand } = await import('execa');
		await execaCommand('bd init --quiet', { cwd, shell: true, timeout: 15000 });
		await execaCommand('bd setup claude', { cwd, shell: true, timeout: 15000 });
	} catch {
		// bd not installed — that's OK, user can install later
	}

	return files;
}

function buildTemplateContext(detected: DetectedStack, answers: Answers): Record<string, unknown> {
	const projectType = answers.projectType;
	const needsFrontend = ['web-app', 'fullstack'].includes(projectType);
	const needsBackend = ['web-app', 'api', 'fullstack', 'microservices'].includes(projectType);

	const agents = ['architect', 'quality', 'security'];
	if (needsFrontend) agents.push('frontend');
	if (needsBackend) agents.push('backend');

	return {
		project: {
			name: answers.projectName,
			preset: answers.preset,
		},
		commands: answers.commands,
		agents,
		has_frontend: needsFrontend,
		has_backend: needsBackend,
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
		// Onboarding context
		onboarding: {
			description: answers.projectDescription,
			projectType: answers.projectType,
			modules: answers.keyModules,
			architecture: answers.architectureStyle,
			sensitivePaths: answers.sensitivePaths,
			domainRules: answers.domainRules,
		},
		has_sensitive: Boolean(answers.sensitivePaths),
		has_domain_rules: Boolean(answers.domainRules),
		has_modules: answers.keyModules.length > 0,
	};
}

function displayResults(files: GeneratedFile[], answers: Answers, specId?: string | null): void {
	const lines = files.map(
		(f) => `  ${chalk.green('✓')} ${f.relativePath}`,
	);

	p.note(lines.join('\n'), `Generated (${files.length} files)`);

	p.log.step('Next steps:');
	p.log.message(`  1. Review ${chalk.cyan('.forge/context/project.md')} — verify your project context`);
	p.log.message(`  2. ${chalk.dim('git add forge.yaml CLAUDE.md .claude .forge')}`);
	p.log.message(`  3. ${chalk.dim('git commit -m "forge: initialize agent harness"')}`);
	if (specId) {
		p.log.message(`  4. Open Claude Code → ${chalk.cyan(`/ingest ${specId}`)}`);
	} else {
		p.log.message(`  4. Open Claude Code → ${chalk.cyan('/deliver "your first task"')}`);
	}
	p.log.message('');
	p.log.message(`  ${chalk.dim('Optional:')}`);
	p.log.message(`    ${chalk.dim('forge add browser-testing')}    — Playwright visual QA`);
	p.log.message(`    ${chalk.dim('forge add compliance-hipaa')}   — HIPAA security checks`);
	p.log.message(`    ${chalk.dim('forge doctor')}                 — Verify harness health`);
}
