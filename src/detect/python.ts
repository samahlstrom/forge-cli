import { join } from 'node:path';
import { exists, readText } from '../utils/fs.js';

type PartialStack = Omit<import('./index.js').DetectedStack, 'features'>;

export async function detectPython(cwd: string): Promise<PartialStack | null> {
	const hasPyproject = await exists(join(cwd, 'pyproject.toml'));
	const hasRequirements = await exists(join(cwd, 'requirements.txt'));

	if (!hasPyproject && !hasRequirements) return null;

	const result: PartialStack = {
		language: 'python',
		framework: null,
		preset: null,
		testRunner: null,
		linter: null,
		typeChecker: null,
		formatter: null,
		packageManager: null,
	};

	// Read deps from available files
	let depsText = '';
	if (hasPyproject) {
		depsText = await readText(join(cwd, 'pyproject.toml'));
	}
	if (hasRequirements) {
		depsText += '\n' + await readText(join(cwd, 'requirements.txt'));
	}

	const hasDep = (name: string) => depsText.includes(name);

	// Framework
	if (hasDep('fastapi')) {
		result.framework = 'fastapi';
		result.preset = 'python-fastapi';
	} else if (hasDep('django')) {
		result.framework = 'django';
		result.preset = 'python-django';
	} else if (hasDep('flask')) {
		result.framework = 'flask';
		result.preset = 'python-flask';
	}

	// Test runner
	if (hasDep('pytest')) {
		result.testRunner = { name: 'pytest', command: 'pytest' };
	} else {
		result.testRunner = { name: 'unittest', command: 'python -m unittest discover' };
	}

	// Linter
	if (hasDep('ruff')) {
		result.linter = { name: 'ruff', command: 'ruff check .' };
	} else if (hasDep('flake8')) {
		result.linter = { name: 'flake8', command: 'flake8' };
	} else if (hasDep('pylint')) {
		result.linter = { name: 'pylint', command: 'pylint src/' };
	}

	// Type checker
	if (hasDep('mypy')) {
		result.typeChecker = { name: 'mypy', command: 'mypy .' };
	} else if (hasDep('pyright')) {
		result.typeChecker = { name: 'pyright', command: 'pyright' };
	}

	// Formatter
	if (hasDep('ruff')) {
		result.formatter = { name: 'ruff', command: 'ruff format .' };
	} else if (hasDep('black')) {
		result.formatter = { name: 'black', command: 'black .' };
	}

	// Package manager
	if (await exists(join(cwd, 'poetry.lock'))) {
		result.packageManager = 'poetry';
	} else if (await exists(join(cwd, 'pdm.lock'))) {
		result.packageManager = 'pdm';
	} else if (await exists(join(cwd, 'uv.lock'))) {
		result.packageManager = 'uv';
	} else {
		result.packageManager = 'pip';
	}

	return result;
}
