import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists, readText } from '../utils/fs.js';
import { bdList, type BdIssue } from '../utils/bd.js';

interface TaskReport {
	task_id: string;
	bead_id: string;
	title: string;
	phase: string;
	agent: string;
	tier: string;
	status: 'success' | 'failure' | 'merge_conflict';
	started_at: string;
	finished_at: string;
	elapsed_ms: number;
	worktree_branch: string;
	files_changed: string[];
	blockers: string[];
	errors: string[];
	summary: string;
}

export async function runStatus(specId: string): Promise<void> {
	const cwd = process.cwd();

	p.intro(chalk.bold('forge run-status') + chalk.dim(` — ${specId}`));

	const specDir = join(cwd, '.forge', 'specs', specId);
	if (!(await exists(join(specDir, 'spec.yaml')))) {
		p.cancel(`No spec.yaml found for ${specId}.`);
		process.exit(1);
	}

	// Get all tasks from bd
	let allTasks: BdIssue[] = [];
	try {
		allTasks = await bdList({ labels: [`spec:${specId}`], type: 'task' }, cwd);
	} catch {
		p.log.warn('Could not query beads — bd may not be initialized.');
	}

	const openTasks = allTasks.filter(t => t.status === 'open' || t.status === 'in_progress');
	const closedTasks = allTasks.filter(t => t.status === 'closed');

	// Read reports if they exist
	const reportsFile = join(specDir, 'reports', 'all-tasks.json');
	let reports: TaskReport[] = [];
	if (await exists(reportsFile)) {
		try {
			reports = JSON.parse(await readText(reportsFile));
		} catch { /* corrupt file */ }
	}

	const succeeded = reports.filter(r => r.status === 'success');
	const failed = reports.filter(r => r.status === 'failure');
	const conflicts = reports.filter(r => r.status === 'merge_conflict');

	// ── Overview ──
	const total = allTasks.length;
	const pct = total > 0 ? Math.round((closedTasks.length / total) * 100) : 0;
	const bar = progressBar(pct, 30);

	const overview = [
		`Progress:  ${bar} ${pct}%`,
		`Total:     ${chalk.cyan(String(total))} tasks`,
		`Closed:    ${chalk.green(String(closedTasks.length))}`,
		`Open:      ${openTasks.length > 0 ? chalk.yellow(String(openTasks.length)) : chalk.dim('0')}`,
		`Reported:  ${chalk.dim(String(reports.length))} (${chalk.green(String(succeeded.length))} ok, ${failed.length > 0 ? chalk.red(String(failed.length)) : chalk.dim('0')} fail, ${conflicts.length > 0 ? chalk.yellow(String(conflicts.length)) : chalk.dim('0')} conflict)`,
	];
	p.note(overview.join('\n'), 'Run Overview');

	// ── Phase breakdown ──
	const phases = new Map<string, { open: number; closed: number; failed: number }>();
	for (const t of allTasks) {
		const phase = t.labels?.find(l => l.startsWith('phase:'))?.replace('phase:', '') ?? '?';
		if (!phases.has(phase)) phases.set(phase, { open: 0, closed: 0, failed: 0 });
		const entry = phases.get(phase)!;
		if (t.status === 'closed') entry.closed++;
		else entry.open++;
	}
	for (const r of reports) {
		if (r.status === 'failure' || r.status === 'merge_conflict') {
			const phase = r.phase.replace('phase:', '');
			if (phases.has(phase)) phases.get(phase)!.failed++;
		}
	}

	if (phases.size > 0) {
		const sorted = [...phases.entries()].sort((a, b) => parseInt(a[0]) - parseInt(b[0]));
		p.log.step(chalk.bold('Phases'));
		for (const [phase, stats] of sorted) {
			const phaseTotal = stats.open + stats.closed;
			const phasePct = phaseTotal > 0 ? Math.round((stats.closed / phaseTotal) * 100) : 0;
			const status = stats.open === 0
				? chalk.green('done')
				: stats.failed > 0
					? chalk.red(`${stats.failed} failed`)
					: chalk.yellow('in progress');
			p.log.message(`  Phase ${chalk.cyan(phase)}: ${progressBar(phasePct, 15)} ${phasePct}% (${stats.closed}/${phaseTotal}) ${status}`);
		}
	}

	// ── Failed tasks ──
	if (failed.length > 0 || conflicts.length > 0) {
		p.log.step(chalk.bold.red('Failed Tasks'));
		for (const r of failed) {
			const errSummary = categorizeError(r.errors);
			p.log.message(`  ${chalk.red('✗')} ${r.title}`);
			p.log.message(`    ${chalk.dim('Error:')} ${errSummary}`);
			if (r.elapsed_ms > 0) {
				p.log.message(`    ${chalk.dim('Elapsed:')} ${formatElapsed(r.elapsed_ms)}`);
			}
		}
		for (const r of conflicts) {
			p.log.message(`  ${chalk.yellow('⚠')} ${r.title}`);
			p.log.message(`    ${chalk.dim('Branch:')} ${chalk.cyan(r.worktree_branch)} (needs manual merge)`);
		}
	}

	// ── Error categories ──
	const allErrors = reports.flatMap(r => r.errors).filter(Boolean);
	if (allErrors.length > 0) {
		const categories = new Map<string, number>();
		for (const err of allErrors) {
			const cat = categorizeError([err]);
			categories.set(cat, (categories.get(cat) ?? 0) + 1);
		}

		p.log.step(chalk.bold('Error Summary'));
		const sorted = [...categories.entries()].sort((a, b) => b[1] - a[1]);
		for (const [cat, count] of sorted) {
			p.log.message(`  ${chalk.red(`${count}x`)} ${cat}`);
		}
	}

	// ── Resumability ──
	p.log.step(chalk.bold('Resume'));
	if (openTasks.length === 0) {
		p.log.success('All tasks complete — nothing to resume.');
	} else {
		let readyCount = 0;
		try {
			const { bdReady } = await import('../utils/bd.js');
			const ready = await bdReady([`spec:${specId}`], cwd, 'task');
			readyCount = ready.length;
		} catch { /* ignore */ }

		p.log.info(`${chalk.cyan(String(openTasks.length))} tasks remaining, ${chalk.green(String(readyCount))} ready to run`);
		if (openTasks.length > 0 && readyCount === 0) {
			p.log.warn(chalk.yellow('All remaining tasks are blocked — check dependencies or merge conflicts.'));
		}
		p.log.message(`  ${chalk.dim('Resume with:')} forge run ${specId} --yes`);
	}

	// ── Muda report pointer ──
	const mudaPath = join(specDir, 'reports', 'muda-analysis.md');
	if (await exists(mudaPath)) {
		p.log.info(`Full analysis: ${chalk.dim(`.forge/specs/${specId}/reports/muda-analysis.md`)}`);
	}

	p.outro('');
}

// ─── Helpers ─────────────────────────────────────────────────────

function progressBar(pct: number, width: number): string {
	const filled = Math.round((pct / 100) * width);
	const empty = width - filled;
	return chalk.green('█'.repeat(filled)) + chalk.dim('░'.repeat(empty));
}

function categorizeError(errors: string[]): string {
	const joined = errors.join(' ');

	if (/hit your limit|rate.?limit|too many requests|429/i.test(joined)) {
		return 'Rate limit / usage cap exceeded';
	}
	if (/timed? ?out|timeout|ETIMEDOUT/i.test(joined)) {
		return 'Timeout (task exceeded 10min)';
	}
	if (/merge conflict/i.test(joined)) {
		return 'Merge conflict';
	}
	if (/ENOENT|not found|no such file/i.test(joined)) {
		return 'File not found';
	}
	if (/permission denied|EACCES/i.test(joined)) {
		return 'Permission denied';
	}
	if (/overloaded/i.test(joined)) {
		return 'API overloaded';
	}
	if (/worktree/i.test(joined)) {
		return 'Git worktree error';
	}

	// Truncate unknown errors
	const first = errors[0] ?? 'Unknown error';
	return first.length > 80 ? first.slice(0, 77) + '...' : first;
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
