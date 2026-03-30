import { Command } from 'commander';
import { init } from './commands/init.js';
import { ingest } from './commands/ingest.js';
import { add } from './commands/add.js';
import { remove } from './commands/remove.js';
import { upgrade } from './commands/upgrade.js';
import { status } from './commands/status.js';
import { doctor } from './commands/doctor.js';
import { seed } from './commands/seed.js';
import { run } from './commands/run.js';

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
	.option('--spec <file>', 'Analyze a spec document to pre-fill project configuration')
	.action(init);

program
	.command('ingest')
	.description('Ingest one or more spec documents for project planning and decomposition')
	.argument('<files...>', 'One or more spec files (PDF, markdown, text)')
	.option('--chunk-size <pages>', 'Pages per chunk for PDF processing', '20')
	.option('--resume <spec-id>', 'Resume analysis of an existing spec')
	.action(ingest);

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

program
	.command('seed <spec-id>')
	.description('Create beads (bd tasks) from an approved spec.yaml decomposition')
	.option('--force', 'Re-create beads even if they already exist')
	.action(seed);

program
	.command('run <spec-id>')
	.description('Auto-pilot: orchestrate task execution from a seeded spec')
	.option('--dry-run', 'Show execution plan without running')
	.option('--phase <number>', 'Only execute tasks in a specific phase')
	.option('--concurrency <number>', 'Max parallel tasks', '1')
	.option('--budget <usd>', 'Max USD spend per task via claude -p')
	.option('--no-review', 'Skip review gates between phases')
	.action(run);

program.parse();
