import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists, readText } from '../utils/fs.js';
import { readYaml } from '../utils/yaml.js';
import { access, constants } from 'node:fs/promises';

interface Check {
	name: string;
	passed: boolean;
	detail?: string;
}

export async function doctor(): Promise<void> {
	const cwd = process.cwd();
	const checks: Check[] = [];

	p.intro(chalk.bold('forge doctor'));

	const spinner = p.spinner();
	spinner.start('Running diagnostics...');

	// 1. forge.yaml exists and is valid
	const forgeYamlPath = join(cwd, 'forge.yaml');
	if (await exists(forgeYamlPath)) {
		try {
			const config = await readYaml<Record<string, unknown>>(forgeYamlPath);
			checks.push({ name: 'forge.yaml', passed: true, detail: `preset: ${(config.project as Record<string, string>)?.preset}` });
		} catch {
			checks.push({ name: 'forge.yaml', passed: false, detail: 'Invalid YAML' });
		}
	} else {
		checks.push({ name: 'forge.yaml', passed: false, detail: 'Not found. Run `forge init`' });
	}

	// 2. CLAUDE.md exists
	checks.push({
		name: 'CLAUDE.md',
		passed: await exists(join(cwd, 'CLAUDE.md')),
		detail: (await exists(join(cwd, 'CLAUDE.md'))) ? undefined : 'Not found',
	});

	// 3. Pipeline files exist
	const pipelineFiles = [
		'orchestrator.sh', 'intake.sh', 'classify.sh', 'decompose.md',
		'execute.md', 'verify.sh', 'deliver.sh', 'bead-state.sh',
	];
	let pipelineMissing = 0;
	for (const file of pipelineFiles) {
		if (!(await exists(join(cwd, '.forge', 'pipeline', file)))) {
			pipelineMissing++;
		}
	}
	checks.push({
		name: 'Pipeline scripts',
		passed: pipelineMissing === 0,
		detail: pipelineMissing > 0 ? `${pipelineMissing} missing` : `${pipelineFiles.length} files OK`,
	});

	// 4. Scripts are executable
	let nonExecutable = 0;
	for (const file of pipelineFiles.filter((f) => f.endsWith('.sh'))) {
		try {
			await access(join(cwd, '.forge', 'pipeline', file), constants.X_OK);
		} catch {
			nonExecutable++;
		}
	}
	const hookFiles = ['pre-edit.sh', 'post-edit.sh', 'session-start.sh'];
	for (const file of hookFiles) {
		try {
			await access(join(cwd, '.forge', 'hooks', file), constants.X_OK);
		} catch {
			nonExecutable++;
		}
	}
	checks.push({
		name: 'Scripts executable',
		passed: nonExecutable === 0,
		detail: nonExecutable > 0
			? `${nonExecutable} scripts not executable. Run: chmod +x .forge/pipeline/*.sh .forge/hooks/*.sh`
			: 'All executable',
	});

	// 5. Agent files exist
	if (await exists(forgeYamlPath)) {
		const config = await readYaml<Record<string, unknown>>(forgeYamlPath);
		const agents = (config.agents as string[]) ?? [];
		let agentsMissing = 0;
		for (const agent of agents) {
			if (!(await exists(join(cwd, '.forge', 'agents', `${agent}.md`)))) {
				agentsMissing++;
			}
		}
		checks.push({
			name: 'Agent definitions',
			passed: agentsMissing === 0,
			detail: agentsMissing > 0 ? `${agentsMissing} missing` : `${agents.length} agents OK`,
		});
	}

	// 6. Context files
	checks.push({
		name: 'context/stack.md',
		passed: await exists(join(cwd, '.forge', 'context', 'stack.md')),
	});
	checks.push({
		name: 'context/project.md',
		passed: await exists(join(cwd, '.forge', 'context', 'project.md')),
	});

	// 7. Beads directory
	checks.push({
		name: 'Beads directory',
		passed:
			(await exists(join(cwd, '.forge', 'beads', 'active'))) &&
			(await exists(join(cwd, '.forge', 'beads', 'archive'))),
	});

	// 8. Claude Code settings
	const settingsPath = join(cwd, '.claude', 'settings.json');
	if (await exists(settingsPath)) {
		try {
			const settings = JSON.parse(await readText(settingsPath));
			const hasHooks = settings.hooks && Object.keys(settings.hooks).length > 0;
			checks.push({
				name: '.claude/settings.json',
				passed: true,
				detail: hasHooks ? 'Hooks registered' : 'No hooks registered',
			});
		} catch {
			checks.push({ name: '.claude/settings.json', passed: false, detail: 'Invalid JSON' });
		}
	} else {
		checks.push({ name: '.claude/settings.json', passed: false, detail: 'Not found' });
	}

	// 9. jq available
	const { execSync } = await import('node:child_process');
	let jqAvailable = false;
	try {
		execSync('which jq', { stdio: 'pipe' });
		jqAvailable = true;
	} catch {}
	checks.push({
		name: 'jq installed',
		passed: jqAvailable,
		detail: jqAvailable ? undefined : 'Required for bead state. Install: brew install jq',
	});

	// 10. gh CLI available
	let ghAvailable = false;
	try {
		execSync('which gh', { stdio: 'pipe' });
		ghAvailable = true;
	} catch {}
	checks.push({
		name: 'gh CLI installed',
		passed: ghAvailable,
		detail: ghAvailable ? undefined : 'Required for PR creation. Install: brew install gh',
	});

	spinner.stop('Diagnostics complete');

	// Display results
	const passed = checks.filter((c) => c.passed).length;
	const failed = checks.filter((c) => !c.passed).length;

	const lines = checks.map((c) => {
		const icon = c.passed ? chalk.green('✓') : chalk.red('✗');
		const detail = c.detail ? chalk.dim(` — ${c.detail}`) : '';
		return `  ${icon} ${c.name}${detail}`;
	});

	p.note(lines.join('\n'), 'Health Check');

	if (failed > 0) {
		p.outro(chalk.yellow(`${passed} passed, ${failed} failed`));
	} else {
		p.outro(chalk.green(`All ${passed} checks passed`));
	}
}
