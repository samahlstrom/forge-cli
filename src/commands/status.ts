import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists } from '../utils/fs.js';
import { readYaml } from '../utils/yaml.js';
import { readHashes } from '../utils/hash.js';

export async function status(): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge status'));

	if (!(await exists(join(cwd, 'forge.yaml')))) {
		p.log.error('No forge.yaml found. Run `forge init` first.');
		process.exit(1);
	}

	const config = await readYaml<Record<string, unknown>>(join(cwd, 'forge.yaml'));
	const project = config.project as Record<string, string>;
	const agents = (config.agents as string[]) ?? [];
	const addons = (config.addons as string[]) ?? [];
	const hashes = await readHashes(cwd);

	// Get task counts from bd
	let openCount = 0;
	let closedCount = 0;
	let bdAvailable = false;
	try {
		const { execSync } = await import('node:child_process');
		const openResult = execSync('bd list --status open --json 2>/dev/null', { stdio: ['pipe', 'pipe', 'pipe'] }).toString().trim();
		openCount = openResult && openResult !== '[]' ? JSON.parse(openResult).length : 0;
		const closedResult = execSync('bd list --status closed --json 2>/dev/null', { stdio: ['pipe', 'pipe', 'pipe'] }).toString().trim();
		closedCount = closedResult && closedResult !== '[]' ? JSON.parse(closedResult).length : 0;
		bdAvailable = true;
	} catch {
		// bd not available
	}

	const lines = [
		`Project:     ${chalk.cyan(project?.name ?? 'unknown')}`,
		`Preset:      ${chalk.cyan(project?.preset ?? 'unknown')}`,
		`Version:     ${chalk.cyan(hashes.version)}`,
		`Agents:      ${chalk.cyan(agents.join(', '))}`,
		`Addons:      ${addons.length > 0 ? chalk.cyan(addons.join(', ')) : chalk.dim('none')}`,
		``,
		`Tracking:    ${bdAvailable ? chalk.green('bd (beads)') : chalk.yellow('bd not installed')}`,
		`Open tasks:  ${openCount > 0 ? chalk.yellow(String(openCount)) : chalk.green('0')}`,
		`Closed:      ${chalk.dim(String(closedCount))}`,
	];

	p.note(lines.join('\n'), 'Harness Status');
	p.outro('');
}
