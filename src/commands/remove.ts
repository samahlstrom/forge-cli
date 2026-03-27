import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists } from '../utils/fs.js';
import { isValidAddon, uninstallAddon } from '../addons/index.js';

export async function remove(addon: string): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold(`forge remove ${addon}`));

	if (!isValidAddon(addon)) {
		p.log.error(`Unknown addon: "${addon}"`);
		process.exit(1);
	}

	if (!(await exists(join(cwd, 'forge.yaml')))) {
		p.log.error('No forge.yaml found. Run `forge init` first.');
		process.exit(1);
	}

	// Check if installed
	const { readYaml } = await import('../utils/yaml.js');
	const config = await readYaml<Record<string, unknown>>(join(cwd, 'forge.yaml'));
	const addons = (config.addons as string[]) ?? [];
	if (!addons.includes(addon)) {
		p.log.warn(`Addon "${addon}" is not installed.`);
		process.exit(0);
	}

	const spinner = p.spinner();
	spinner.start(`Removing ${addon}...`);

	try {
		const removedFiles = await uninstallAddon(addon, cwd);

		for (const file of removedFiles) {
			p.log.success(`Deleted ${file}`);
		}
		p.log.success('Updated forge.yaml');

		spinner.stop(`${addon} removed`);
	} catch (err) {
		spinner.stop('Removal failed');
		p.log.error(String(err));
		process.exit(1);
	}

	p.outro(chalk.green('Done!'));
}
