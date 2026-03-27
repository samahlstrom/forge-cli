import { parse, stringify } from 'yaml';
import { readText, writeText } from './fs.js';

export async function readYaml<T = unknown>(path: string): Promise<T> {
	const text = await readText(path);
	return parse(text) as T;
}

export async function writeYaml(path: string, data: unknown): Promise<void> {
	const text = stringify(data, { lineWidth: 120 });
	await writeText(path, text);
}

export { parse as parseYaml, stringify as stringifyYaml } from 'yaml';
