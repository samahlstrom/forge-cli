import { join } from 'node:path';
import { readText } from './fs.js';
import { bdCreate, bdLink } from './bd.js';

interface SpecTask {
	id: string;
	title: string;
	description: string;
	risk_tier: string;
	dependencies: string[];
	files_likely: string[];
	agent: string;
}

interface SpecFeature {
	id: string;
	title: string;
	tasks: SpecTask[];
}

interface SpecEpic {
	id: string;
	domain: string;
	title: string;
	features: SpecFeature[];
}

interface SpecPhase {
	id: string;
	name: string;
	epics: string[];
	rationale: string;
	parallelizable: boolean;
}

interface SpecYaml {
	spec_id: string;
	status: string;
	summary: string;
	epics: SpecEpic[];
	execution_plan: {
		phases: SpecPhase[];
		total_tasks: number;
	};
}

export interface SeedResult {
	phases: number;
	epics: number;
	tasks: number;
	links: number;
	taskMap: Record<string, string>; // spec task id -> bd id
}

export async function seedBeads(specDir: string, specId: string, cwd?: string): Promise<SeedResult> {
	const { parse } = await import('yaml');
	const specContent = await readText(join(specDir, 'spec.yaml'));
	const spec: SpecYaml = parse(specContent);

	const taskMap: Record<string, string> = {};  // spec task id -> bd id
	const epicMap: Record<string, string> = {};  // spec epic id -> bd id
	const phaseEpicIds: string[] = [];           // bd ids for phase-level epics
	let linkCount = 0;

	const effectiveCwd = cwd ?? process.cwd();

	// Create phase epics and their child epics + tasks
	for (let pi = 0; pi < spec.execution_plan.phases.length; pi++) {
		const phase = spec.execution_plan.phases[pi];
		const phaseNum = pi + 1;

		// Create phase-level epic
		const phaseBeadId = await bdCreate({
			title: `Phase ${phaseNum}: ${phase.name}`,
			type: 'epic',
			labels: [`spec:${specId}`, `phase:${phaseNum}`],
			metadata: { spec_phase_id: phase.id, rationale: phase.rationale },
		}, effectiveCwd);
		phaseEpicIds.push(phaseBeadId);

		// Phase ordering: phase N+1 is blocked by phase N
		if (pi > 0) {
			await bdLink(phaseBeadId, phaseEpicIds[pi - 1], 'blocks', effectiveCwd);
			linkCount++;
		}

		// Create epics within this phase
		for (const epicId of phase.epics) {
			const epic = spec.epics.find(e => e.id === epicId);
			if (!epic) continue;

			const epicBeadId = await bdCreate({
				title: epic.title,
				type: 'epic',
				parent: phaseBeadId,
				labels: [`spec:${specId}`, `phase:${phaseNum}`, `domain:${epic.domain}`],
			}, effectiveCwd);
			epicMap[epic.id] = epicBeadId;

			// Create tasks for each feature in this epic
			for (const feature of epic.features) {
				for (const task of feature.tasks) {
					const taskBeadId = await bdCreate({
						title: task.title,
						description: task.description,
						parent: epicBeadId,
						labels: [
							`spec:${specId}`,
							`phase:${phaseNum}`,
							`tier:${task.risk_tier}`,
							`agent:${task.agent}`,
							`feature:${feature.id}`,
						],
						metadata: {
							spec_task_id: task.id,
							files_likely: task.files_likely,
						},
					}, effectiveCwd);
					taskMap[task.id] = taskBeadId;
				}
			}
		}
	}

	// Wire up task-level dependencies
	for (const epic of spec.epics) {
		for (const feature of epic.features) {
			for (const task of feature.tasks) {
				if (!task.dependencies?.length) continue;
				const thisBeadId = taskMap[task.id];
				if (!thisBeadId) continue;

				for (const depId of task.dependencies) {
					const depBeadId = taskMap[depId];
					if (!depBeadId) continue;
					await bdLink(thisBeadId, depBeadId, 'blocks', effectiveCwd);
					linkCount++;
				}
			}
		}
	}

	return {
		phases: spec.execution_plan.phases.length,
		epics: Object.keys(epicMap).length,
		tasks: Object.keys(taskMap).length,
		links: linkCount,
		taskMap,
	};
}
