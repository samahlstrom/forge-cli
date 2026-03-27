import { join } from 'node:path';
import { exists, readJson } from '../utils/fs.js';

interface PackageJson {
	dependencies?: Record<string, string>;
	devDependencies?: Record<string, string>;
}

type PartialStack = Omit<import('./index.js').DetectedStack, 'features'>;

function hasDep(pkg: PackageJson, name: string): boolean {
	return Boolean(pkg.dependencies?.[name] || pkg.devDependencies?.[name]);
}

export async function detectNode(cwd: string): Promise<PartialStack | null> {
	const pkgPath = join(cwd, 'package.json');
	if (!(await exists(pkgPath))) return null;

	const pkg = await readJson<PackageJson>(pkgPath);

	const result: PartialStack = {
		language: hasDep(pkg, 'typescript') || (await exists(join(cwd, 'tsconfig.json')))
			? 'typescript'
			: 'javascript',
		framework: null,
		preset: null,
		testRunner: null,
		linter: null,
		typeChecker: null,
		formatter: null,
		packageManager: null,
	};

	// Framework detection
	if (hasDep(pkg, '@sveltejs/kit')) {
		result.framework = 'sveltekit';
		result.preset = 'sveltekit-ts';
	} else if (hasDep(pkg, 'next')) {
		result.framework = 'next';
		result.preset = 'react-next-ts';
	} else if (hasDep(pkg, 'nuxt')) {
		result.framework = 'nuxt';
		result.preset = 'vue-nuxt-ts';
	} else if (hasDep(pkg, 'vue')) {
		result.framework = 'vue';
		result.preset = 'vue-nuxt-ts';
	} else if (hasDep(pkg, 'express')) {
		result.framework = 'express';
		result.preset = 'node-express';
	} else if (hasDep(pkg, 'fastify')) {
		result.framework = 'fastify';
		result.preset = 'node-express';
	}

	// Test runner
	if (hasDep(pkg, 'vitest')) {
		result.testRunner = { name: 'vitest', command: 'npx vitest run' };
	} else if (hasDep(pkg, 'jest')) {
		result.testRunner = { name: 'jest', command: 'npx jest' };
	} else if (hasDep(pkg, 'mocha')) {
		result.testRunner = { name: 'mocha', command: 'npx mocha' };
	}

	// Linter
	if (hasDep(pkg, 'eslint')) {
		result.linter = { name: 'eslint', command: 'npx eslint .' };
	} else if (hasDep(pkg, '@biomejs/biome')) {
		result.linter = { name: 'biome', command: 'npx biome check .' };
	}

	// Type checker
	if (await exists(join(cwd, 'tsconfig.json'))) {
		if (result.framework === 'sveltekit') {
			result.typeChecker = { name: 'svelte-check', command: 'npm run check' };
		} else if (result.framework === 'next') {
			result.typeChecker = { name: 'tsc', command: 'npx tsc --noEmit' };
		} else {
			result.typeChecker = { name: 'tsc', command: 'npx tsc --noEmit' };
		}
	}

	// Formatter
	if (hasDep(pkg, 'prettier')) {
		result.formatter = { name: 'prettier', command: 'npx prettier --write' };
	} else if (hasDep(pkg, '@biomejs/biome')) {
		result.formatter = { name: 'biome', command: 'npx biome format --write .' };
	}

	// Package manager
	if (await exists(join(cwd, 'pnpm-lock.yaml'))) {
		result.packageManager = 'pnpm';
	} else if (await exists(join(cwd, 'yarn.lock'))) {
		result.packageManager = 'yarn';
	} else if (await exists(join(cwd, 'bun.lockb'))) {
		result.packageManager = 'bun';
	} else {
		result.packageManager = 'npm';
	}

	return result;
}
