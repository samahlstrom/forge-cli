import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists, listDir } from '../utils/fs.js';
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

	// Count active beads
	const activeBeads = await listDir(join(cwd, '.forge', 'beads', 'active'));
	const beadCount = activeBeads.filter((f) => f.endsWith('.json')).length;

	// Count archived beads
	const archivedBeads = await listDir(join(cwd, '.forge', 'beads', 'archive'));
	const archiveCount = archivedBeads.filter((f) => f.endsWith('.json')).length;

	const lines = [
		`Project:     ${chalk.cyan(project?.name ?? 'unknown')}`,
		`Preset:      ${chalk.cyan(project?.preset ?? 'unknown')}`,
		`Version:     ${chalk.cyan(hashes.version)}`,
		`Agents:      ${chalk.cyan(agents.join(', '))}`,
		`Addons:      ${addons.length > 0 ? chalk.cyan(addons.join(', ')) : chalk.dim('none')}`,
		``,
		`Active beads:   ${beadCount > 0 ? chalk.yellow(String(beadCount)) : chalk.green('0')}`,
		`Archived beads: ${chalk.dim(String(archiveCount))}`,
	];

	p.note(lines.join('\n'), 'Harness Status');
	p.outro('');
}
