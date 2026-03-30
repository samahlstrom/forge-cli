import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists, readText, writeText, ensureDir } from '../utils/fs.js';
import { readHashes, writeHashes, hashContent, hashFile, type HashManifest } from '../utils/hash.js';
import { readYaml } from '../utils/yaml.js';
import { render, type TemplateContext } from '../render/engine.js';
import { mergeForgeYaml } from '../render/merge.js';
import { resolveTemplatePath } from '../utils/fs.js';
import { detect } from '../detect/index.js';
import { dirname } from 'node:path';

interface UpgradeOptions {
	force?: boolean;
}

// Files that are ALWAYS overwritten (tool-owned)
const ALWAYS_OVERWRITE = ['.forge/context/stack.md'];

// Files that are NEVER touched (user-owned)
const NEVER_TOUCH = ['.forge/context/project.md'];

export async function upgrade(options: UpgradeOptions): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge upgrade'));

	if (!(await exists(join(cwd, 'forge.yaml')))) {
		p.log.error('No forge.yaml found. Run `forge init` first.');
		process.exit(1);
	}

	const hashes = await readHashes(cwd);
	const currentVersion = hashes.version;
	const newVersion = '0.1.0'; // TODO: read from package.json

	p.log.info(`Current: v${currentVersion} → Available: v${newVersion}`);

	const config = await readYaml<Record<string, unknown>>(join(cwd, 'forge.yaml'));
	const project = config.project as Record<string, string>;
	const preset = project?.preset ?? 'sveltekit-ts';

	// Rebuild template context
	const detected = await detect(cwd);
	const ctx = buildUpgradeContext(config, detected, preset);

	let updated = 0;
	let skipped = 0;
	let prompted = 0;
	const newHashes: HashManifest = { version: newVersion, files: {} };

	// Get all template files that should exist
	for (const [relativePath, installedHash] of Object.entries(hashes.files)) {
		// Skip addon files — they're managed separately
		if (relativePath.startsWith('.forge/addons/')) continue;

		// Never touch user-owned files
		if (NEVER_TOUCH.includes(relativePath)) {
			p.log.message(`  ${chalk.dim('⊘')} ${relativePath} ${chalk.dim('— user-owned, skipped')}`);
			skipped++;
			// Keep the existing hash
			newHashes.files[relativePath] = installedHash;
			continue;
		}

		const filePath = join(cwd, relativePath);
		if (!(await exists(filePath))) {
			// File was deleted by user — skip
			skipped++;
			continue;
		}

		// Try to find and render the new template
		const templatePath = findTemplatePath(relativePath, preset);
		if (!templatePath) {
			newHashes.files[relativePath] = installedHash;
			continue;
		}

		let newContent: string;
		try {
			const template = await readText(resolveTemplatePath(templatePath));
			newContent = render(template, ctx);
		} catch {
			newHashes.files[relativePath] = installedHash;
			continue;
		}

		const newContentHash = hashContent(newContent);

		// Always overwrite tool-owned files
		if (ALWAYS_OVERWRITE.includes(relativePath)) {
			await writeText(filePath, newContent);
			newHashes.files[relativePath] = newContentHash;
			p.log.success(`${relativePath} ${chalk.dim('— tool-owned, updated')}`);
			updated++;
			continue;
		}

		// Check if user has modified the file
		const currentHash = await hashFile(filePath);
		const userModified = currentHash !== installedHash;

		if (!userModified || options.force) {
			// User hasn't modified — safe to overwrite
			await writeText(filePath, newContent);
			newHashes.files[relativePath] = newContentHash;
			if (newContentHash !== installedHash) {
				p.log.success(`${relativePath} ${chalk.dim('— updated')}`);
				updated++;
			}
		} else {
			// User modified — ask
			const action = await p.select({
				message: `${relativePath} — you modified this. What to do?`,
				options: [
					{ value: 'skip', label: 'Skip (keep your version)' },
					{ value: 'overwrite', label: 'Overwrite (use new version)' },
				],
			});

			if (p.isCancel(action)) {
				p.cancel('Upgrade cancelled.');
				process.exit(0);
			}

			if (action === 'overwrite') {
				await writeText(filePath, newContent);
				newHashes.files[relativePath] = newContentHash;
				updated++;
			} else {
				newHashes.files[relativePath] = currentHash;
				skipped++;
			}
			prompted++;
		}
	}

	// Merge forge.yaml — add new fields, preserve existing
	try {
		const templatePath = resolveTemplatePath('core', 'forge.yaml.hbs');
		const template = await readText(templatePath);
		const newForgeYaml = render(template, ctx);
		const existingForgeYaml = await readText(join(cwd, 'forge.yaml'));
		const merged = mergeForgeYaml(existingForgeYaml, newForgeYaml);
		if (merged !== existingForgeYaml) {
			await writeText(join(cwd, 'forge.yaml'), merged);
			p.log.success('forge.yaml — merged new fields');
			updated++;
		}
	} catch {
		// Merge failed — skip
	}

	await writeHashes(cwd, newHashes);

	p.outro(
		`Updated to v${newVersion}. ${chalk.green(`${updated} updated`)}, ${chalk.dim(`${skipped} skipped`)}${prompted > 0 ? `, ${chalk.yellow(`${prompted} prompted`)}` : ''}`,
	);
}

function findTemplatePath(relativePath: string, preset: string): string | null {
	// Map output paths back to template paths
	const mappings: Record<string, string> = {
		'forge.yaml': 'core/forge.yaml.hbs',
		'CLAUDE.md': 'core/CLAUDE.md.hbs',
		'.claude/settings.json': 'core/settings.json.hbs',
		'.claude/skills/deliver/SKILL.md': 'core/skill-deliver.md.hbs',
		'.forge/pipeline/orchestrator.sh': 'core/pipeline/orchestrator.sh.hbs',
		'.forge/pipeline/intake.sh': 'core/pipeline/intake.sh.hbs',
		'.forge/pipeline/classify.sh': 'core/pipeline/classify.sh.hbs',
		'.forge/pipeline/decompose.md': 'core/pipeline/decompose.md.hbs',
		'.forge/pipeline/execute.md': 'core/pipeline/execute.md.hbs',
		'.forge/pipeline/verify.sh': 'core/pipeline/verify.sh.hbs',
		'.forge/pipeline/deliver.sh': 'core/pipeline/deliver.sh.hbs',
		'.forge/agents/architect.md': 'core/agents/architect.md.hbs',
		'.forge/agents/quality.md': 'core/agents/quality.md.hbs',
		'.forge/agents/security.md': 'core/agents/security.md.hbs',
		'.forge/agents/frontend.md': 'core/agents/frontend.md.hbs',
		'.forge/agents/backend.md': 'core/agents/backend.md.hbs',
		'.forge/context/stack.md': `presets/${preset}/stack.md.hbs`,
		'.forge/context/project.md': 'core/context/project.md.hbs',
		'.forge/hooks/pre-edit.sh': 'core/hooks/pre-edit.sh.hbs',
		'.forge/hooks/post-edit.sh': 'core/hooks/post-edit.sh.hbs',
		'.forge/hooks/session-start.sh': 'core/hooks/session-start.sh.hbs',
	};

	return mappings[relativePath] ?? null;
}

function buildUpgradeContext(
	config: Record<string, unknown>,
	detected: import('../detect/index.js').DetectedStack,
	preset: string,
): TemplateContext {
	const project = config.project as Record<string, string>;
	const commands = config.commands as Record<string, string>;
	const agents = (config.agents as string[]) ?? [];
	const pipeline = config.pipeline as Record<string, unknown> ?? {};
	const isFrontendPreset = ['sveltekit-ts', 'react-next-ts', 'vue-nuxt-ts'].includes(preset);

	return {
		project: {
			name: project?.name ?? 'my-app',
			preset,
		},
		commands: commands ?? {},
		agents,
		has_frontend: isFrontendPreset,
		has_format: Boolean(commands?.format),
		auto_pr: pipeline?.auto_pr ?? true,
		detected: {
			language: detected.language,
			framework: detected.framework,
			features: detected.features,
		},
		preset,
		is_sveltekit: preset === 'sveltekit-ts',
		is_nextjs: preset === 'react-next-ts',
		is_fastapi: preset === 'python-fastapi',
		is_go: preset === 'go',
	};
}
