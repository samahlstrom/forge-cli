import { parse, stringify } from 'yaml';

/**
 * Merge new forge.yaml fields into existing config without overwriting user values.
 * Adds new keys with defaults, preserves existing values.
 */
export function mergeForgeYaml(existing: string, updated: string): string {
	const existingDoc = parse(existing) as Record<string, unknown>;
	const updatedDoc = parse(updated) as Record<string, unknown>;

	const merged = deepMergeNewOnly(existingDoc, updatedDoc);
	return stringify(merged, { lineWidth: 120 });
}

/**
 * Deep merge that only adds keys from `source` that don't exist in `target`.
 * Never overwrites existing values.
 */
function deepMergeNewOnly(
	target: Record<string, unknown>,
	source: Record<string, unknown>,
): Record<string, unknown> {
	const result = { ...target };

	for (const key of Object.keys(source)) {
		if (!(key in result)) {
			result[key] = source[key];
		} else if (
			isPlainObject(result[key]) &&
			isPlainObject(source[key])
		) {
			result[key] = deepMergeNewOnly(
				result[key] as Record<string, unknown>,
				source[key] as Record<string, unknown>,
			);
		}
		// If key exists in target and isn't an object merge case, keep target's value
	}

	return result;
}

function isPlainObject(val: unknown): val is Record<string, unknown> {
	return typeof val === 'object' && val !== null && !Array.isArray(val);
}
