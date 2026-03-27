import { access, mkdir, readFile, writeFile, readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';

export async function exists(path: string): Promise<boolean> {
	try {
		await access(path);
		return true;
	} catch {
		return false;
	}
}

export async function ensureDir(path: string): Promise<void> {
	await mkdir(path, { recursive: true });
}

export async function readText(path: string): Promise<string> {
	return readFile(path, 'utf-8');
}

export async function writeText(path: string, content: string): Promise<void> {
	await ensureDir(dirname(path));
	await writeFile(path, content, 'utf-8');
}

export async function readJson<T = unknown>(path: string): Promise<T> {
	const text = await readText(path);
	return JSON.parse(text) as T;
}

export async function writeJson(path: string, data: unknown): Promise<void> {
	await writeText(path, JSON.stringify(data, null, '\t') + '\n');
}

export async function listDir(path: string): Promise<string[]> {
	try {
		return await readdir(path);
	} catch {
		return [];
	}
}

export function resolveTemplatePath(...segments: string[]): string {
	// Templates are relative to the package root, not the dist/ dir
	const packageRoot = join(import.meta.dirname, '..', '..');
	return join(packageRoot, 'templates', ...segments);
}
