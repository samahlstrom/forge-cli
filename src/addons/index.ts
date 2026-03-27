import { join } from 'node:path';
import { parse } from 'yaml';
import { readText, exists, writeText, ensureDir } from '../utils/fs.js';
import { resolveTemplatePath } from '../utils/fs.js';
import { readYaml, writeYaml } from '../utils/yaml.js';
import { hashContent, readHashes, writeHashes } from '../utils/hash.js';


export interface AddonManifest {
	name: string;
	description: string;
	version: number;
	requires?: {
		commands?: Record<string, boolean>;
	};
	patches: {
		forge_yaml?: Record<string, unknown>;
		agents?: string[];
	};
	files: Array<{
		source: string;
		target: string;
	}>;
	post_install?: string[];
}

const ADDON_NAMES = ['browser-testing', 'compliance-hipaa', 'compliance-soc2', 'beads-dolt-backend'] as const;
export type AddonName = (typeof ADDON_NAMES)[number];

export function isValidAddon(name: string): name is AddonName {
	return (ADDON_NAMES as readonly string[]).includes(name);
}

export function listAvailableAddons(): readonly string[] {
	return ADDON_NAMES;
}

export async function getAddonManifest(addonName: string): Promise<AddonManifest> {
	const manifestPath = resolveTemplatePath('addons', addonName, 'manifest.yaml');
	const text = await readText(manifestPath);
	return parse(text) as AddonManifest;
}

export async function installAddon(addonName: string, cwd: string): Promise<string[]> {
	const manifest = await getAddonManifest(addonName);
	const installedFiles: string[] = [];

	// Check requirements
	if (manifest.requires?.commands) {
		const forgeYaml = await readYaml<Record<string, unknown>>(join(cwd, 'forge.yaml'));
		const commands = forgeYaml.commands as Record<string, string> | undefined;
		for (const [cmd, required] of Object.entries(manifest.requires.commands)) {
			if (required && (!commands || !commands[cmd])) {
				throw new Error(
					`Addon "${addonName}" requires the "${cmd}" command in forge.yaml. Add it and try again.`,
				);
			}
		}
	}

	// Copy files
	for (const file of manifest.files) {
		const sourcePath = resolveTemplatePath('addons', addonName, 'files', file.source);
		const targetPath = join(cwd, file.target);
		const content = await readText(sourcePath);
		await ensureDir(join(targetPath, '..'));
		await writeText(targetPath, content);
		installedFiles.push(file.target);
	}

	// Patch forge.yaml
	await patchForgeYaml(cwd, manifest, 'add');

	// Update hashes
	const hashes = await readHashes(cwd);
	for (const file of manifest.files) {
		const targetPath = join(cwd, file.target);
		const content = await readText(targetPath);
		hashes.files[file.target] = hashContent(content);
	}
	await writeHashes(cwd, hashes);

	return installedFiles;
}

export async function uninstallAddon(addonName: string, cwd: string): Promise<string[]> {
	const manifest = await getAddonManifest(addonName);
	const removedFiles: string[] = [];
	const { unlink } = await import('node:fs/promises');

	// Remove files
	for (const file of manifest.files) {
		const targetPath = join(cwd, file.target);
		if (await exists(targetPath)) {
			await unlink(targetPath);
			removedFiles.push(file.target);
		}
	}

	// Unpatch forge.yaml
	await patchForgeYaml(cwd, manifest, 'remove');

	// Remove from hashes
	const hashes = await readHashes(cwd);
	for (const file of manifest.files) {
		delete hashes.files[file.target];
	}
	await writeHashes(cwd, hashes);

	return removedFiles;
}

async function patchForgeYaml(
	cwd: string,
	manifest: AddonManifest,
	action: 'add' | 'remove',
): Promise<void> {
	const yamlPath = join(cwd, 'forge.yaml');
	const config = await readYaml<Record<string, unknown>>(yamlPath);

	// Patch specific forge_yaml fields
	if (manifest.patches.forge_yaml) {
		for (const [dotPath, value] of Object.entries(manifest.patches.forge_yaml)) {
			if (dotPath === 'addons') {
				// Handle addons array specially
				const addons = (config.addons as string[]) ?? [];
				const addonEntry = (value as string[])[0]?.replace('+', '');
				if (action === 'add' && addonEntry && !addons.includes(addonEntry)) {
					addons.push(addonEntry);
				} else if (action === 'remove' && addonEntry) {
					const idx = addons.indexOf(addonEntry);
					if (idx >= 0) addons.splice(idx, 1);
				}
				config.addons = addons;
			} else {
				// Set nested value via dot path
				setNestedValue(config, dotPath, action === 'add' ? value : getDefaultForType(value));
			}
		}
	}

	// Patch agents array
	if (manifest.patches.agents) {
		const agents = (config.agents as string[]) ?? [];
		for (const agentEntry of manifest.patches.agents) {
			const agentName = agentEntry.replace('+', '');
			if (action === 'add' && !agents.includes(agentName)) {
				agents.push(agentName);
			} else if (action === 'remove') {
				const idx = agents.indexOf(agentName);
				if (idx >= 0) agents.splice(idx, 1);
			}
		}
		config.agents = agents;
	}

	await writeYaml(yamlPath, config);
}

function setNestedValue(obj: Record<string, unknown>, dotPath: string, value: unknown): void {
	const keys = dotPath.split('.');
	let current = obj;
	for (let i = 0; i < keys.length - 1; i++) {
		if (!(keys[i] in current) || typeof current[keys[i]] !== 'object') {
			current[keys[i]] = {};
		}
		current = current[keys[i]] as Record<string, unknown>;
	}
	current[keys[keys.length - 1]] = value;
}

function getDefaultForType(value: unknown): unknown {
	if (typeof value === 'boolean') return false;
	if (typeof value === 'string') return '';
	if (typeof value === 'number') return 0;
	return null;
}
