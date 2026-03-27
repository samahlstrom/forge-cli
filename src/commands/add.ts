import * as p from '@clack/prompts';
import chalk from 'chalk';
import { execa } from 'execa';
import { join } from 'node:path';
import { exists } from '../utils/fs.js';
import { isValidAddon, listAvailableAddons, getAddonManifest, installAddon } from '../addons/index.js';

export async function add(addon: string): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold(`forge add ${addon}`));

	// Validate addon name
	if (!isValidAddon(addon)) {
		p.log.error(`Unknown addon: "${addon}"`);
		p.log.message(`Available addons: ${listAvailableAddons().join(', ')}`);
		process.exit(1);
	}

	// Check forge.yaml exists
	if (!(await exists(join(cwd, 'forge.yaml')))) {
		p.log.error('No forge.yaml found. Run `forge init` first.');
		process.exit(1);
	}

	// Check if already installed
	const { readYaml } = await import('../utils/yaml.js');
	const config = await readYaml<Record<string, unknown>>(join(cwd, 'forge.yaml'));
	const addons = (config.addons as string[]) ?? [];
	if (addons.includes(addon)) {
		p.log.warn(`Addon "${addon}" is already installed.`);
		process.exit(0);
	}

	const spinner = p.spinner();
	spinner.start(`Installing ${addon}...`);

	try {
		const installedFiles = await installAddon(addon, cwd);

		for (const file of installedFiles) {
			p.log.success(`Created ${file}`);
		}
		p.log.success(`Updated forge.yaml`);

		// Run post-install commands
		const manifest = await getAddonManifest(addon);
		if (manifest.post_install) {
			for (const cmd of manifest.post_install) {
				spinner.message(`Running: ${cmd}`);
				try {
					await execa(cmd, { shell: true, cwd, stdio: 'pipe' });
					p.log.success(`Ran: ${cmd}`);
				} catch {
					p.log.warn(`Post-install command failed: ${cmd}`);
					p.log.message('  You may need to run this manually.');
				}
			}
		}

		spinner.stop(`${addon} installed`);
	} catch (err) {
		spinner.stop('Installation failed');
		p.log.error(String(err));
		process.exit(1);
	}

	p.outro(chalk.green('Done!'));
}
