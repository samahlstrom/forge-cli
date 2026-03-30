import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists } from '../utils/fs.js';
import { seedBeads } from '../utils/seed-beads.js';
import { bdCount } from '../utils/bd.js';

interface SeedOptions {
	force?: boolean;
}

export async function seed(specId: string, options: SeedOptions): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge seed') + chalk.dim(` — ${specId}`));

	const specDir = join(cwd, '.forge', 'specs', specId);

	// Verify spec.yaml exists
	const specYamlPath = join(specDir, 'spec.yaml');
	if (!(await exists(specYamlPath))) {
		p.cancel(`No spec.yaml found at ${specYamlPath}. Run /ingest ${specId} first.`);
		process.exit(1);
	}

	// Check if beads already exist for this spec
	try {
		const existingCount = await bdCount({ labels: [`spec:${specId}`] }, cwd);
		if (existingCount > 0 && !options.force) {
			const overwrite = await p.confirm({
				message: `${existingCount} beads already exist for ${specId}. Re-create?`,
				initialValue: false,
			});
			if (p.isCancel(overwrite) || !overwrite) {
				p.cancel('Seed cancelled.');
				process.exit(0);
			}
		}
	} catch {
		// bd not initialized or count failed — proceed
	}

	const spinner = p.spinner();
	spinner.start('Creating beads from spec.yaml...');

	try {
		const result = await seedBeads(specDir, specId, cwd);
		spinner.stop('Beads created');

		const lines = [
			`Phases:  ${chalk.cyan(String(result.phases))} epics`,
			`Epics:   ${chalk.cyan(String(result.epics))} epics`,
			`Tasks:   ${chalk.cyan(String(result.tasks))} tasks`,
			`Deps:    ${chalk.cyan(String(result.links))} blocking links`,
		];
		p.note(lines.join('\n'), 'Beads seeded');

		p.log.step('Next:');
		p.log.message(`  Run ${chalk.cyan(`forge run ${specId}`)} to start auto-pilot execution`);
		p.log.message(`  Or ${chalk.cyan('bd ready')} to see what tasks are available`);
	} catch (err) {
		spinner.stop('Seeding failed');
		p.log.error(String(err));
		process.exit(1);
	}

	p.outro(chalk.green('Ready to execute.'));
}
