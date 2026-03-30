import * as p from '@clack/prompts';
import chalk from 'chalk';
import { join } from 'node:path';
import { exists, readText, writeText, ensureDir } from '../utils/fs.js';
import { bdReady, bdClose, bdUpdate, bdList, type BdIssue } from '../utils/bd.js';
import { execaCommand } from 'execa';
import { writeFile, unlink, readFile } from 'node:fs/promises';

interface RunOptions {
	dryRun?: boolean;
	phase?: string;
	concurrency?: string;
	budget?: string;
	review?: boolean;
	yes?: boolean;
}

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

interface PhaseReport {
	phase: string;
	tasks: TaskReport[];
	started_at: string;
	finished_at: string;
	success_count: number;
	failure_count: number;
}

export async function run(specId: string, options: RunOptions): Promise<void> {
	const cwd = process.cwd();
	const concurrency = parseInt(options.concurrency ?? '1', 10);
	const budget = options.budget ? parseFloat(options.budget) : undefined;
	const reviewEnabled = options.review !== false && !options.yes;

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
	p.log.warn(chalk.yellow('Auto-pilot uses --dangerously-skip-permissions and git worktrees for isolation.'));
	if (concurrency > 1) {
		p.log.info(`Concurrency: ${chalk.cyan(String(concurrency))} parallel worktrees`);
	}
	if (!options.yes) {
		const confirm = await p.confirm({
			message: 'Continue?',
			initialValue: true,
		});
		if (p.isCancel(confirm) || !confirm) {
			p.cancel('Run cancelled.');
			process.exit(0);
		}
	}

	// Set up reports directory
	const reportsDir = join(cwd, '.forge', 'specs', specId, 'reports');
	await ensureDir(reportsDir);

	// Main orchestration loop
	let completed = 0;
	let failed = 0;
	let currentPhase = '';
	const startTime = Date.now();
	const allReports: TaskReport[] = [];
	const phaseReports: PhaseReport[] = [];
	let currentPhaseReport: PhaseReport | null = null;

	while (true) {
		// Get ready tasks
		const labelFilters = [`spec:${specId}`];
		if (options.phase) labelFilters.push(`phase:${options.phase}`);

		let readyTasks: BdIssue[];
		try {
			readyTasks = await bdReady(labelFilters, cwd, 'task');
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

			// Re-check ready tasks
			try {
				readyTasks = await bdReady(labelFilters, cwd, 'task');
			} catch {
				readyTasks = [];
			}

			if (readyTasks.length === 0) {
				// Finalize current phase report
				if (currentPhaseReport) {
					currentPhaseReport.finished_at = new Date().toISOString();
					phaseReports.push(currentPhaseReport);
					await writePhaseReport(reportsDir, currentPhaseReport);
				}

				if (options.yes) {
					// Auto-pilot: continue automatically
					p.log.info(chalk.dim(`Phase transition — ${remaining.length} tasks unblocking...`));
				} else if (reviewEnabled) {
					const elapsed = formatElapsed(Date.now() - startTime);
					p.log.info(`\n${chalk.bold('Phase checkpoint')} — ${completed} completed, ${failed} failed, ${remaining.length} remaining (${elapsed})`);

					// Show phase retrospective if we have reports
					if (currentPhaseReport && currentPhaseReport.tasks.length > 0) {
						showPhaseRetro(currentPhaseReport);
					}

					const cont = await p.confirm({
						message: `${remaining.length} tasks blocked. Continue to next phase?`,
						initialValue: true,
					});
					if (p.isCancel(cont) || !cont) break;
				} else {
					break;
				}

				currentPhaseReport = null;
			}
		}

		if (readyTasks.length === 0) continue;

		// Detect phase transition
		const taskPhase = readyTasks[0].labels?.find(l => l.startsWith('phase:')) ?? '';
		if (taskPhase !== currentPhase) {
			if (currentPhase && currentPhaseReport) {
				currentPhaseReport.finished_at = new Date().toISOString();
				phaseReports.push(currentPhaseReport);
				await writePhaseReport(reportsDir, currentPhaseReport);

				if (reviewEnabled) {
					p.log.success(`\n${chalk.bold('Phase complete:')} ${currentPhase}`);
					showPhaseRetro(currentPhaseReport);

					const cont = await p.confirm({
						message: `Start ${taskPhase}?`,
						initialValue: true,
					});
					if (p.isCancel(cont) || !cont) break;
				}
			}
			currentPhase = taskPhase;
			currentPhaseReport = {
				phase: currentPhase,
				tasks: [],
				started_at: new Date().toISOString(),
				finished_at: '',
				success_count: 0,
				failure_count: 0,
			};
			p.log.step(chalk.bold(`\nStarting ${currentPhase}`));
		}

		// Pick tasks up to concurrency limit
		const batch = readyTasks.slice(0, concurrency);

		// Execute batch in parallel worktrees
		const results = await Promise.allSettled(
			batch.map(task => executeTaskInWorktree(task, specId, cwd, budget))
		);

		for (let i = 0; i < results.length; i++) {
			const result = results[i];
			const task = batch[i];

			if (result.status === 'fulfilled') {
				const report = result.value;
				allReports.push(report);
				currentPhaseReport?.tasks.push(report);

				if (report.status === 'success') {
					completed++;
					currentPhaseReport && currentPhaseReport.success_count++;
					p.log.success(`${chalk.green('✓')} ${task.title} ${chalk.dim(`(${formatElapsed(report.elapsed_ms)})`)}`);
					try {
						await bdClose(task.id, cwd);
					} catch (err) {
						p.log.warn(`Failed to close ${task.id}: ${err}`);
					}
				} else if (report.status === 'merge_conflict') {
					failed++;
					currentPhaseReport && currentPhaseReport.failure_count++;
					p.log.warn(`${chalk.yellow('⚠')} ${task.title}: merge conflict — branch ${chalk.cyan(report.worktree_branch)} preserved`);
					try {
						await bdUpdate(task.id, { status: 'open' }, cwd);
					} catch { /* ignore */ }
				} else {
					failed++;
					currentPhaseReport && currentPhaseReport.failure_count++;
					p.log.error(`${chalk.red('✗')} ${task.title}: ${report.errors.join('; ')}`);
					try {
						await bdUpdate(task.id, { status: 'open' }, cwd);
					} catch { /* ignore */ }
				}
			} else {
				failed++;
				currentPhaseReport && currentPhaseReport.failure_count++;
				const report: TaskReport = {
					task_id: (task.metadata as Record<string, unknown>)?.spec_task_id as string ?? task.id,
					bead_id: task.id,
					title: task.title,
					phase: currentPhase,
					agent: task.labels?.find(l => l.startsWith('agent:'))?.replace('agent:', '') ?? '',
					tier: task.labels?.find(l => l.startsWith('tier:'))?.replace('tier:', '') ?? '',
					status: 'failure',
					started_at: new Date().toISOString(),
					finished_at: new Date().toISOString(),
					elapsed_ms: 0,
					worktree_branch: '',
					files_changed: [],
					blockers: [`Unhandled error: ${result.reason}`],
					errors: [String(result.reason)],
					summary: `Task failed with unhandled error: ${result.reason}`,
				};
				allReports.push(report);
				currentPhaseReport?.tasks.push(report);
				p.log.error(`${chalk.red('✗')} ${task.title}: ${result.reason}`);
				try {
					await bdUpdate(task.id, { status: 'open' }, cwd);
				} catch { /* ignore */ }
			}
		}

		// Write incremental reports
		await writeText(join(reportsDir, 'all-tasks.json'), JSON.stringify(allReports, null, 2));
	}

	// Finalize last phase
	if (currentPhaseReport && !currentPhaseReport.finished_at) {
		currentPhaseReport.finished_at = new Date().toISOString();
		phaseReports.push(currentPhaseReport);
		await writePhaseReport(reportsDir, currentPhaseReport);
	}

	// Generate final Muda analysis
	const elapsed = formatElapsed(Date.now() - startTime);
	await generateMudaAnalysis(reportsDir, allReports, phaseReports, elapsed);

	const summaryLines = [
		`Completed:  ${chalk.green(String(completed))}`,
		`Failed:     ${failed > 0 ? chalk.red(String(failed)) : chalk.dim('0')}`,
		`Elapsed:    ${chalk.cyan(elapsed)}`,
		`Reports:    ${chalk.dim(`.forge/specs/${specId}/reports/`)}`,
	];
	p.note(summaryLines.join('\n'), 'Run complete');

	if (failed > 0) {
		p.log.info(`Review blockers: ${chalk.cyan(`.forge/specs/${specId}/reports/muda-analysis.md`)}`);
	}

	p.outro(chalk.green('Done.'));
}

// ─── Worktree Execution ───────────────────────────────────────────

async function executeTaskInWorktree(
	task: BdIssue,
	specId: string,
	cwd: string,
	budget?: number,
): Promise<TaskReport> {
	const startedAt = new Date();
	const tier = task.labels?.find(l => l.startsWith('tier:'))?.replace('tier:', '') ?? 'T2';
	const agent = task.labels?.find(l => l.startsWith('agent:'))?.replace('agent:', '') ?? '';
	const phase = task.labels?.find(l => l.startsWith('phase:'))?.replace('phase:', '') ?? '';
	const filesLikely = (task.metadata as Record<string, unknown>)?.files_likely as string[] | undefined;
	const specTaskId = (task.metadata as Record<string, unknown>)?.spec_task_id as string | undefined;

	// Create a unique branch for this task
	const branchName = `forge/${specId}/${task.id}`;
	const worktreePath = join(cwd, '.forge', 'worktrees', task.id);

	const report: TaskReport = {
		task_id: specTaskId ?? task.id,
		bead_id: task.id,
		title: task.title,
		phase: `phase:${phase}`,
		agent,
		tier,
		status: 'failure',
		started_at: startedAt.toISOString(),
		finished_at: '',
		elapsed_ms: 0,
		worktree_branch: branchName,
		files_changed: [],
		blockers: [],
		errors: [],
		summary: '',
	};

	try {
		// Clean up any stale worktree/branch from a previous failed run
		try {
			await execaCommand(`git worktree remove ${worktreePath} --force`, { shell: true, cwd, timeout: 10000 });
		} catch { /* doesn't exist, fine */ }
		try {
			await execaCommand(`git branch -D ${branchName}`, { shell: true, cwd, timeout: 5000 });
		} catch { /* doesn't exist, fine */ }
		await execaCommand(`git worktree prune`, { shell: true, cwd, timeout: 5000 });

		// Create worktree
		await execaCommand(`git worktree add -b ${branchName} ${worktreePath} HEAD`, {
			shell: true, cwd, timeout: 30000,
		});

		// Build the deliver prompt with retrospective instruction
		const filesStr = filesLikely?.length ? ` Target files: ${filesLikely.join(', ')}.` : '';
		const reportPath = join(worktreePath, '.forge', 'task-report.json');

		const prompt = buildTaskPrompt(task, specId, filesStr, tier, agent, specTaskId, reportPath);

		// Write prompt to temp file
		const tmpFile = join('/tmp', `forge-run-${task.id}-${Date.now()}.txt`);
		await writeFile(tmpFile, prompt);

		try {
			const args = ['claude', '-p', '--dangerously-skip-permissions', '--output-format', 'json'];
			if (budget) args.push('--max-budget-usd', String(budget));

			await execaCommand(
				`cat "${tmpFile}" | ${args.join(' ')}`,
				{ shell: true, cwd: worktreePath, timeout: 600000 },
			);

			// Read the agent's self-report if it wrote one
			try {
				const agentReport = JSON.parse(await readFile(reportPath, 'utf-8'));
				report.blockers = agentReport.blockers ?? [];
				report.errors = agentReport.errors ?? [];
				report.summary = agentReport.summary ?? '';
			} catch {
				report.summary = 'Task completed (no agent report generated)';
			}

			// Get list of files changed
			try {
				const diffResult = await execaCommand('git diff --name-only HEAD', {
					shell: true, cwd: worktreePath, timeout: 10000,
				});
				report.files_changed = diffResult.stdout.trim().split('\n').filter(Boolean);
			} catch { /* ignore */ }

			// Commit changes in worktree
			try {
				await execaCommand('git add -A', { shell: true, cwd: worktreePath, timeout: 10000 });
				const commitMsg = `forge: ${task.title} [${specTaskId ?? task.id}]`;
				await execaCommand(`git commit -m "${commitMsg}" --allow-empty`, {
					shell: true, cwd: worktreePath, timeout: 15000,
				});
			} catch {
				// Nothing to commit
			}

			// Merge worktree branch back to main
			try {
				const mainBranch = await getMainBranch(cwd);
				await execaCommand(`git checkout ${mainBranch}`, { shell: true, cwd, timeout: 10000 });
				await execaCommand(`git merge ${branchName} --no-edit`, { shell: true, cwd, timeout: 30000 });
				report.status = 'success';
			} catch (mergeErr) {
				// Merge conflict — abort and preserve branch for manual resolution
				try {
					await execaCommand('git merge --abort', { shell: true, cwd, timeout: 10000 });
				} catch { /* ignore */ }
				report.status = 'merge_conflict';
				report.blockers.push(`Merge conflict on branch ${branchName}`);
				report.errors.push(String(mergeErr));
			}
		} finally {
			try { await unlink(tmpFile); } catch { /* ignore */ }
		}
	} catch (err) {
		report.errors.push(String(err));
		report.summary = `Worktree setup or execution failed: ${err}`;
	} finally {
		// Clean up worktree (but keep the branch if merge failed)
		try {
			await execaCommand(`git worktree remove ${worktreePath} --force`, {
				shell: true, cwd, timeout: 15000,
			});
		} catch { /* ignore */ }

		// Delete branch only on success
		if (report.status === 'success') {
			try {
				await execaCommand(`git branch -D ${branchName}`, {
					shell: true, cwd, timeout: 10000,
				});
			} catch { /* ignore */ }
		}

		const finishedAt = new Date();
		report.finished_at = finishedAt.toISOString();
		report.elapsed_ms = finishedAt.getTime() - startedAt.getTime();
	}

	return report;
}

function buildTaskPrompt(
	task: BdIssue,
	specId: string,
	filesStr: string,
	tier: string,
	agent: string,
	specTaskId: string | undefined,
	reportPath: string,
): string {
	return `/deliver "${task.title} — ${task.description ?? ''}.${filesStr} Risk: ${tier}. Agent: ${agent}. Spec ref: ${specTaskId ?? task.id}"

IMPORTANT: Before you finish, write a JSON report to "${reportPath}" with this structure:
{
  "summary": "1-2 sentence summary of what you accomplished",
  "blockers": ["list of things that blocked progress or required workarounds"],
  "errors": ["list of errors encountered during execution"],
  "decisions": ["key decisions you made and why"],
  "suggestions": ["improvements for future similar tasks"]
}
If everything went smoothly, blockers and errors should be empty arrays. Always write this file before finishing.`;
}

// ─── Reporting ────────────────────────────────────────────────────

function showPhaseRetro(phaseReport: PhaseReport): void {
	const failedTasks = phaseReport.tasks.filter(t => t.status !== 'success');
	const blockers = phaseReport.tasks.flatMap(t => t.blockers).filter(Boolean);

	if (failedTasks.length === 0 && blockers.length === 0) {
		p.log.info(chalk.dim('  No issues in this phase.'));
		return;
	}

	if (failedTasks.length > 0) {
		p.log.warn(`  ${chalk.yellow(`${failedTasks.length} failed tasks:`)}`);
		for (const t of failedTasks) {
			p.log.message(`    ${chalk.red('✗')} ${t.title}: ${t.errors[0] ?? t.status}`);
		}
	}

	if (blockers.length > 0) {
		const unique = [...new Set(blockers)];
		p.log.warn(`  ${chalk.yellow(`${unique.length} blockers identified:`)}`);
		for (const b of unique) {
			p.log.message(`    ${chalk.dim('•')} ${b}`);
		}
	}
}

async function writePhaseReport(reportsDir: string, phaseReport: PhaseReport): Promise<void> {
	const filename = `${phaseReport.phase.replace(':', '-')}.json`;
	await writeText(join(reportsDir, filename), JSON.stringify(phaseReport, null, 2));
}

async function generateMudaAnalysis(
	reportsDir: string,
	allReports: TaskReport[],
	phaseReports: PhaseReport[],
	totalElapsed: string,
): Promise<void> {
	const totalTasks = allReports.length;
	const successCount = allReports.filter(r => r.status === 'success').length;
	const failCount = allReports.filter(r => r.status === 'failure').length;
	const conflictCount = allReports.filter(r => r.status === 'merge_conflict').length;

	// Collect all blockers and errors
	const allBlockers = allReports.flatMap(r => r.blockers).filter(Boolean);
	const allErrors = allReports.flatMap(r => r.errors).filter(Boolean);
	const allSuggestions = allReports.flatMap(r => {
		const meta = r as unknown as Record<string, unknown>;
		return (meta.suggestions as string[]) ?? [];
	}).filter(Boolean);

	// Categorize waste
	const blockerFreq: Record<string, number> = {};
	for (const b of allBlockers) {
		const normalized = b.slice(0, 80);
		blockerFreq[normalized] = (blockerFreq[normalized] ?? 0) + 1;
	}

	const errorFreq: Record<string, number> = {};
	for (const e of allErrors) {
		const normalized = e.slice(0, 80);
		errorFreq[normalized] = (errorFreq[normalized] ?? 0) + 1;
	}

	// Find slowest tasks
	const sorted = [...allReports].sort((a, b) => b.elapsed_ms - a.elapsed_ms);
	const slowest = sorted.slice(0, 5);

	// Agent performance
	const agentStats: Record<string, { success: number; fail: number; totalMs: number }> = {};
	for (const r of allReports) {
		if (!agentStats[r.agent]) agentStats[r.agent] = { success: 0, fail: 0, totalMs: 0 };
		agentStats[r.agent].totalMs += r.elapsed_ms;
		if (r.status === 'success') agentStats[r.agent].success++;
		else agentStats[r.agent].fail++;
	}

	// Build markdown report
	const lines: string[] = [
		'# Muda Analysis — Waste & Blocker Report',
		'',
		`Generated: ${new Date().toISOString()}`,
		`Total elapsed: ${totalElapsed}`,
		'',
		'## Summary',
		'',
		`| Metric | Count |`,
		`|--------|-------|`,
		`| Total tasks | ${totalTasks} |`,
		`| Succeeded | ${successCount} |`,
		`| Failed | ${failCount} |`,
		`| Merge conflicts | ${conflictCount} |`,
		`| Success rate | ${totalTasks > 0 ? Math.round((successCount / totalTasks) * 100) : 0}% |`,
		'',
	];

	// Blockers section
	if (Object.keys(blockerFreq).length > 0) {
		lines.push('## Blockers (Muda — Waiting Waste)');
		lines.push('');
		lines.push('Recurring blockers indicate systemic issues that slow the pipeline.');
		lines.push('');
		const sortedBlockers = Object.entries(blockerFreq).sort((a, b) => b[1] - a[1]);
		for (const [blocker, count] of sortedBlockers) {
			lines.push(`- **${count}x** ${blocker}`);
		}
		lines.push('');
	}

	// Errors section
	if (Object.keys(errorFreq).length > 0) {
		lines.push('## Errors (Muda — Defect Waste)');
		lines.push('');
		lines.push('Recurring errors suggest missing prerequisites, bad assumptions, or spec gaps.');
		lines.push('');
		const sortedErrors = Object.entries(errorFreq).sort((a, b) => b[1] - a[1]);
		for (const [error, count] of sortedErrors) {
			lines.push(`- **${count}x** ${error}`);
		}
		lines.push('');
	}

	// Slowest tasks
	if (slowest.length > 0) {
		lines.push('## Slowest Tasks (Muda — Processing Waste)');
		lines.push('');
		lines.push('Tasks taking disproportionately long may need decomposition or better context.');
		lines.push('');
		for (const t of slowest) {
			lines.push(`- **${formatElapsed(t.elapsed_ms)}** ${t.title} (${t.tier}, ${t.agent})`);
		}
		lines.push('');
	}

	// Agent performance
	if (Object.keys(agentStats).length > 0) {
		lines.push('## Agent Performance');
		lines.push('');
		lines.push('| Agent | Success | Failed | Avg Time |');
		lines.push('|-------|---------|--------|----------|');
		for (const [agent, stats] of Object.entries(agentStats)) {
			const total = stats.success + stats.fail;
			const avg = total > 0 ? formatElapsed(Math.round(stats.totalMs / total)) : '-';
			lines.push(`| ${agent} | ${stats.success} | ${stats.fail} | ${avg} |`);
		}
		lines.push('');
	}

	// Merge conflicts
	const conflicts = allReports.filter(r => r.status === 'merge_conflict');
	if (conflicts.length > 0) {
		lines.push('## Merge Conflicts (Muda — Motion Waste)');
		lines.push('');
		lines.push('Conflicts indicate tasks touching overlapping files. Consider:');
		lines.push('- Reducing concurrency for tightly coupled epics');
		lines.push('- Reordering tasks to serialize shared-file work');
		lines.push('');
		for (const c of conflicts) {
			lines.push(`- **${c.title}** — branch \`${c.worktree_branch}\` preserved for manual merge`);
		}
		lines.push('');
	}

	// Suggestions from agents
	const uniqueSuggestions = [...new Set(allSuggestions)];
	if (uniqueSuggestions.length > 0) {
		lines.push('## Agent Suggestions (Kaizen — Continuous Improvement)');
		lines.push('');
		for (const s of uniqueSuggestions) {
			lines.push(`- ${s}`);
		}
		lines.push('');
	}

	// Phase breakdown
	if (phaseReports.length > 0) {
		lines.push('## Phase Breakdown');
		lines.push('');
		for (const pr of phaseReports) {
			const phaseElapsed = pr.finished_at && pr.started_at
				? formatElapsed(new Date(pr.finished_at).getTime() - new Date(pr.started_at).getTime())
				: '?';
			lines.push(`### ${pr.phase} (${phaseElapsed})`);
			lines.push('');
			lines.push(`- ${pr.success_count} succeeded, ${pr.failure_count} failed`);
			const phaseBlockers = pr.tasks.flatMap(t => t.blockers).filter(Boolean);
			if (phaseBlockers.length > 0) {
				lines.push(`- Blockers: ${[...new Set(phaseBlockers)].join('; ')}`);
			}
			lines.push('');
		}
	}

	await writeText(join(reportsDir, 'muda-analysis.md'), lines.join('\n'));
}

// ─── Utilities ────────────────────────────────────────────────────

async function showDryRun(specId: string, cwd: string): Promise<void> {
	const ready = await bdReady([`spec:${specId}`], cwd, 'task');
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

async function getMainBranch(cwd: string): Promise<string> {
	try {
		const result = await execaCommand('git symbolic-ref refs/remotes/origin/HEAD', {
			shell: true, cwd, timeout: 5000,
		});
		return result.stdout.trim().replace('refs/remotes/origin/', '');
	} catch {
		// Fallback: check if main or master exists
		try {
			await execaCommand('git rev-parse --verify main', { shell: true, cwd, timeout: 5000 });
			return 'main';
		} catch {
			return 'master';
		}
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
