/**
 * Minimal template rendering engine.
 * Supports: {{var}}, {{nested.var}}, {{#if cond}}...{{/if}}, {{#unless cond}}...{{/unless}}, {{#each items}}...{{/each}}
 */

export interface TemplateContext {
	[key: string]: unknown;
}

function resolve(path: string, ctx: TemplateContext): unknown {
	return path.split('.').reduce<unknown>((obj, key) => {
		if (obj != null && typeof obj === 'object') {
			return (obj as Record<string, unknown>)[key];
		}
		return undefined;
	}, ctx);
}

function isTruthy(val: unknown): boolean {
	if (Array.isArray(val)) return val.length > 0;
	return Boolean(val);
}

export function render(template: string, ctx: TemplateContext): string {
	let result = template;

	// Process {{#each items}}...{{/each}} blocks
	// Consume leading/trailing whitespace when tags are on their own line
	result = result.replace(
		/[ \t]*\{\{#each\s+(\S+?)\}\}\n?([\s\S]*?)[ \t]*\{\{\/each\}\}\n?/g,
		(_match, key: string, body: string) => {
			const items = resolve(key, ctx);
			if (!Array.isArray(items)) return '';
			return items
				.map((item, index) => {
					const itemCtx: TemplateContext = {
						...ctx,
						'.': item,
						'@index': index,
						'@first': index === 0,
						'@last': index === items.length - 1,
					};
					if (typeof item === 'object' && item !== null) {
						Object.assign(itemCtx, item);
					} else {
						itemCtx['this'] = item;
					}
					return render(body, itemCtx);
				})
				.join('');
		},
	);

	// Process {{#if cond}}...{{else}}...{{/if}} blocks
	result = result.replace(
		/[ \t]*\{\{#if\s+(\S+?)\}\}\n?([\s\S]*?)[ \t]*\{\{\/if\}\}\n?/g,
		(_match, key: string, body: string) => {
			const parts = body.split(/[ \t]*\{\{else\}\}\n?/);
			const val = resolve(key, ctx);
			if (isTruthy(val)) {
				return render(parts[0], ctx);
			}
			return parts[1] ? render(parts[1], ctx) : '';
		},
	);

	// Process {{#unless cond}}...{{/unless}} blocks
	result = result.replace(
		/[ \t]*\{\{#unless\s+(\S+?)\}\}\n?([\s\S]*?)[ \t]*\{\{\/unless\}\}\n?/g,
		(_match, key: string, body: string) => {
			const val = resolve(key, ctx);
			if (!isTruthy(val)) {
				return render(body, ctx);
			}
			return '';
		},
	);

	// Process {{variable}} substitutions
	result = result.replace(/\{\{(\S+?)\}\}/g, (_match, key: string) => {
		const val = resolve(key, ctx);
		if (val === undefined || val === null) return '';
		return String(val);
	});

	return result;
}
