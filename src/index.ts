import { Command } from 'commander';
import { init } from './commands/init.js';
import { add } from './commands/add.js';
import { remove } from './commands/remove.js';
import { upgrade } from './commands/upgrade.js';
import { status } from './commands/status.js';
import { doctor } from './commands/doctor.js';

const program = new Command();

program
	.name('forge')
	.description('Agent harness scaffolding for Claude Code')
	.version('0.1.0');

program
	.command('init')
	.description('Initialize a new agent harness in the current project')
	.option('--preset <preset>', 'Skip detection and use a specific preset')
	.option('--force', 'Overwrite existing harness files')
	.option('--yes', 'Accept all defaults without prompting')
	.action(init);

program
	.command('add <addon>')
	.description('Install an optional addon (e.g., browser-testing, compliance-hipaa)')
	.action(add);

program
	.command('remove <addon>')
	.description('Remove an installed addon')
	.action(remove);

program
	.command('upgrade')
	.description('Upgrade core pipeline and addons to the latest version')
	.option('--force', 'Overwrite all files without prompting')
	.action(upgrade);

program
	.command('status')
	.description('Show installed preset, active addons, and pipeline health')
	.action(status);

program
	.command('doctor')
	.description('Diagnose harness health: check files, scripts, deps, and config')
	.action(doctor);

program.parse();
