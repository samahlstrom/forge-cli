import { createHash } from 'node:crypto';
import { readText, writeJson, readJson, exists } from './fs.js';
import { join } from 'node:path';

export interface HashManifest {
	version: string;
	files: Record<string, string>;
}

export function hashContent(content: string): string {
	return 'sha256:' + createHash('sha256').update(content).digest('hex');
}

export async function hashFile(path: string): Promise<string> {
	const content = await readText(path);
	return hashContent(content);
}

const HASHES_FILE = '.forge/.hashes.json';

export async function readHashes(projectRoot: string): Promise<HashManifest> {
	const path = join(projectRoot, HASHES_FILE);
	if (await exists(path)) {
		return readJson<HashManifest>(path);
	}
	return { version: '0.0.0', files: {} };
}

export async function writeHashes(
	projectRoot: string,
	manifest: HashManifest,
): Promise<void> {
	await writeJson(join(projectRoot, HASHES_FILE), manifest);
}
