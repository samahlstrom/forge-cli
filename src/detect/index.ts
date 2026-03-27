import { detectNode } from './node.js';
import { detectPython } from './python.js';
import { detectGo } from './go.js';
import { detectFeatures, type FeatureFlags } from './features.js';

export interface DetectedStack {
	language: 'typescript' | 'javascript' | 'python' | 'go' | 'unknown';
	framework: string | null;
	preset: string | null;
	testRunner: { name: string; command: string } | null;
	linter: { name: string; command: string } | null;
	typeChecker: { name: string; command: string } | null;
	formatter: { name: string; command: string } | null;
	packageManager: string | null;
	features: FeatureFlags;
}

export async function detect(cwd: string): Promise<DetectedStack> {
	const features = await detectFeatures(cwd);

	// Try each detector in order of likelihood
	const nodeResult = await detectNode(cwd);
	if (nodeResult) {
		return { ...nodeResult, features };
	}

	const pythonResult = await detectPython(cwd);
	if (pythonResult) {
		return { ...pythonResult, features };
	}

	const goResult = await detectGo(cwd);
	if (goResult) {
		return { ...goResult, features };
	}

	return {
		language: 'unknown',
		framework: null,
		preset: null,
		testRunner: null,
		linter: null,
		typeChecker: null,
		formatter: null,
		packageManager: null,
		features,
	};
}
