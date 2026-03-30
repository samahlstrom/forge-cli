import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists, readText } from '../utils/fs.js';
import { bdReady, bdClose, bdUpdate, bdList, bdShow, type BdIssue } from '../utils/bd.js';
import { execaCommand } from 'execa';

interface RunOptions {
	dryRun?: boolean;
	phase?: string;
	concurrency?: string;
	budget?: string;
	review?: boolean;
}

export async function run(specId: string, options: RunOptions): Promise<void> {
	const cwd = process.cwd();
	const concurrency = parseInt(options.concurrency ?? '1', 10);
	const budget = options.budget ? parseFloat(options.budget) : undefined;
	const reviewEnabled = options.review !== false;

	p.intro(chalk.bold('forge run') + chalk.dim(` — ${specId}`));

	// Verify spec exists
	const specDir = join(cwd, '.forge', 'specs', specId);
	if (!(await exists(join(specDir, 'spec.yaml')))) {
		p.cancel(`No spec.yaml found. Run /ingest ${specId} first.`);
		process.exit(1);
	}

	// Verify beads exist
	let allTasks: BdIssue[];
	try {
		allTasks = await bdList({ labels: [`spec:${specId}`], type: 'task' }, cwd);
	} catch {
		p.cancel('Could not query beads. Is bd initialized? Run: forge seed ' + specId);
		process.exit(1);
	}

	if (allTasks.length === 0) {
		p.cancel(`No task beads found for ${specId}. Run: forge seed ${specId}`);
		process.exit(1);
	}

	const openTasks = allTasks.filter(t => t.status === 'open' || t.status === 'in_progress');
	p.log.info(`${chalk.cyan(String(openTasks.length))} tasks remaining out of ${allTasks.length} total`);

	if (options.dryRun) {
		await showDryRun(specId, cwd);
		p.outro(chalk.dim('Dry run complete — no tasks executed.'));
		return;
	}

	// Warn about permissions
	p.log.warn(chalk.yellow('Auto-pilot mode uses --dangerously-skip-permissions for unattended execution.'));
	const confirm = await p.confirm({
		message: 'Continue?',
		initialValue: true,
	});
	if (p.isCancel(confirm) || !confirm) {
		p.cancel('Run cancelled.');
		process.exit(0);
	}

	// Main orchestration loop
	let completed = 0;
	let failed = 0;
	let currentPhase = '';
	const startTime = Date.now();

	while (true) {
		// Get ready tasks
		const labelFilters = [`spec:${specId}`];
		if (options.phase) labelFilters.push(`phase:${options.phase}`);

		let readyTasks: BdIssue[];
		try {
			readyTasks = await bdReady(labelFilters, cwd);
		} catch {
			readyTasks = [];
		}

		if (readyTasks.length === 0) {
			// Check if there are still open tasks (blocked)
			const remaining = await bdList({ labels: [`spec:${specId}`], type: 'task', status: 'open' }, cwd);
			if (remaining.length === 0) break; // All done

			// Try closing eligible epics to unblock next phase
			try {
				await execaCommand('bd epic close-eligible', { shell: true, cwd, timeout: 10000 });
			} catch { /* ignore */ }

			// Re-check ready tasks after epic closing
			try {
				readyTasks = await bdReady(labelFilters, cwd);
			} catch {
				readyTasks = [];
			}

			if (readyTasks.length === 0) {
				// Phase boundary or deadlock
				if (reviewEnabled) {
					const elapsed = formatElapsed(Date.now() - startTime);
					p.log.info(`\n${chalk.bold('Phase checkpoint')} — ${completed} completed, ${failed} failed, ${remaining.length} remaining (${elapsed})`);

					const cont = await p.confirm({
						message: `${remaining.length} tasks blocked. Continue waiting for unblock?`,
						initialValue: false,
					});
					if (p.isCancel(cont) || !cont) break;
				} else {
					break; // No review mode, just stop
				}
			}
		}

		if (readyTasks.length === 0) continue;

		// Detect phase transition
		const taskPhase = readyTasks[0].labels?.find(l => l.startsWith('phase:')) ?? '';
		if (taskPhase !== currentPhase) {
			if (currentPhase && reviewEnabled) {
				p.log.success(`\n${chalk.bold('Phase complete:')} ${currentPhase}`);
				const cont = await p.confirm({
					message: `Start ${taskPhase}?`,
					initialValue: true,
				});
				if (p.isCancel(cont) || !cont) break;
			}
			currentPhase = taskPhase;
			p.log.step(chalk.bold(`\nStarting ${currentPhase}`));
		}

		// Pick tasks up to concurrency limit
		const batch = readyTasks.slice(0, concurrency);

		// Execute batch
		const results = await Promise.allSettled(
			batch.map(task => executeTask(task, specId, cwd, budget))
		);

		for (let i = 0; i < results.length; i++) {
			const result = results[i];
			const task = batch[i];
			if (result.status === 'fulfilled' && result.value) {
				completed++;
				p.log.success(`${chalk.green('✓')} ${task.title}`);
				try {
					await bdClose(task.id, cwd);
				} catch (err) {
					p.log.warn(`Failed to close ${task.id}: ${err}`);
				}
			} else {
				failed++;
				const reason = result.status === 'rejected' ? result.reason : 'Task returned failure';
				p.log.error(`${chalk.red('✗')} ${task.title}: ${reason}`);
				try {
					await bdUpdate(task.id, { status: 'open' }, cwd);
				} catch { /* ignore */ }
			}
		}
	}

	// Final summary
	const elapsed = formatElapsed(Date.now() - startTime);
	const summaryLines = [
		`Completed:  ${chalk.green(String(completed))}`,
		`Failed:     ${failed > 0 ? chalk.red(String(failed)) : chalk.dim('0')}`,
		`Elapsed:    ${chalk.cyan(elapsed)}`,
	];
	p.note(summaryLines.join('\n'), 'Run complete');
	p.outro(chalk.green('Done.'));
}

async function executeTask(
	task: BdIssue,
	specId: string,
	cwd: string,
	budget?: number,
): Promise<boolean> {
	const tier = task.labels?.find(l => l.startsWith('tier:'))?.replace('tier:', '') ?? 'T2';
	const agent = task.labels?.find(l => l.startsWith('agent:'))?.replace('agent:', '') ?? '';
	const filesLikely = (task.metadata as Record<string, unknown>)?.files_likely as string[] | undefined;
	const specTaskId = (task.metadata as Record<string, unknown>)?.spec_task_id as string | undefined;

	const filesStr = filesLikely?.length ? ` Target files: ${filesLikely.join(', ')}.` : '';
	const prompt = `/deliver "${task.title} — ${task.description ?? ''}.${filesStr} Risk: ${tier}. Agent: ${agent}. Spec ref: ${specTaskId ?? task.id}"`;

	const args = ['claude', '-p', '--dangerously-skip-permissions', '--output-format', 'json'];
	if (budget) args.push('--max-budget-usd', String(budget));

	try {
		// Write prompt to temp file to avoid shell escaping issues
		const tmpFile = join('/tmp', `forge-run-${task.id}-${Date.now()}.txt`);
		const { writeFile, unlink } = await import('node:fs/promises');
		await writeFile(tmpFile, prompt);

		try {
			const result = await execaCommand(
				`cat "${tmpFile}" | ${args.join(' ')}`,
				{ shell: true, cwd, timeout: 600000 }, // 10 min per task
			);

			// Parse result
			let success = true;
			try {
				const parsed = JSON.parse(result.stdout);
				if (parsed.is_error) success = false;
			} catch {
				// Non-JSON output, assume success if exit code was 0
			}
			return success;
		} finally {
			try { const { unlink: ul } = await import('node:fs/promises'); await ul(tmpFile); } catch { /* ignore */ }
		}
	} catch (err) {
		throw new Error(`claude -p failed: ${String(err)}`);
	}
}

async function showDryRun(specId: string, cwd: string): Promise<void> {
	const ready = await bdReady([`spec:${specId}`], cwd);
	if (ready.length === 0) {
		p.log.info('No ready tasks found.');
		return;
	}

	p.log.step(`${ready.length} tasks ready to execute:`);
	for (const task of ready) {
		const tier = task.labels?.find(l => l.startsWith('tier:'))?.replace('tier:', '') ?? '?';
		const phase = task.labels?.find(l => l.startsWith('phase:'))?.replace('phase:', '') ?? '?';
		p.log.message(`  ${chalk.dim(`P${phase}`)} ${chalk.cyan(tier)} ${task.title}`);
	}
}

function formatElapsed(ms: number): string {
	const secs = Math.floor(ms / 1000);
	if (secs < 60) return `${secs}s`;
	const mins = Math.floor(secs / 60);
	const remSecs = secs % 60;
	if (mins < 60) return `${mins}m ${remSecs}s`;
	const hrs = Math.floor(mins / 60);
	const remMins = mins % 60;
	return `${hrs}h ${remMins}m`;
}
