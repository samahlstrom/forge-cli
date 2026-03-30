import { execaCommand } from 'execa';

export interface BdIssue {
	id: string;
	title: string;
	description: string;
	status: string;
	labels: string[];
	priority: number;
	parent?: string;
	metadata?: Record<string, unknown>;
}

export interface BdCreateOpts {
	title: string;
	description?: string;
	type?: string;
	labels?: string[];
	parent?: string;
	metadata?: Record<string, unknown>;
	deps?: string[];
}

export async function bdCreate(opts: BdCreateOpts, cwd?: string): Promise<string> {
	const args = ['bd', 'create', JSON.stringify(opts.title)];
	if (opts.description) args.push('-d', JSON.stringify(opts.description));
	if (opts.type) args.push('-t', opts.type);
	if (opts.labels?.length) args.push('-l', opts.labels.join(','));
	if (opts.parent) args.push('--parent', opts.parent);
	if (opts.metadata) args.push('--metadata', JSON.stringify(JSON.stringify(opts.metadata)));
	if (opts.deps?.length) args.push('--deps', opts.deps.join(','));
	args.push('--json');

	const result = await execaCommand(args.join(' '), { shell: true, cwd, timeout: 15000 });
	const parsed = JSON.parse(result.stdout);
	return parsed.id ?? parsed.ID ?? parsed.issue_id ?? result.stdout.trim();
}

export async function bdLink(from: string, to: string, type = 'blocks', cwd?: string): Promise<void> {
	await execaCommand(`bd link ${from} ${to} --type ${type}`, { shell: true, cwd, timeout: 10000 });
}

export async function bdReady(labels?: string[], cwd?: string): Promise<BdIssue[]> {
	const args = ['bd', 'ready', '--json', '-n', '100'];
	if (labels?.length) args.push('-l', labels.join(','));
	const result = await execaCommand(args.join(' '), { shell: true, cwd, timeout: 10000 });
	if (!result.stdout.trim()) return [];
	try {
		const parsed = JSON.parse(result.stdout);
		return Array.isArray(parsed) ? parsed : [parsed];
	} catch {
		return [];
	}
}

export async function bdClose(id: string, cwd?: string): Promise<void> {
	await execaCommand(`bd close ${id}`, { shell: true, cwd, timeout: 10000 });
}

export async function bdUpdate(id: string, opts: { assignee?: string; labels?: string[]; status?: string }, cwd?: string): Promise<void> {
	const args = ['bd', 'update', id];
	if (opts.assignee) args.push('-a', opts.assignee);
	if (opts.status) args.push('-s', opts.status);
	args.push('--json');
	await execaCommand(args.join(' '), { shell: true, cwd, timeout: 10000 });
}

export async function bdList(filters: { labels?: string[]; type?: string; status?: string }, cwd?: string): Promise<BdIssue[]> {
	const args = ['bd', 'list', '--json'];
	if (filters.labels?.length) args.push('-l', filters.labels.join(','));
	if (filters.type) args.push('-t', filters.type);
	if (filters.status) args.push('-s', filters.status);
	const result = await execaCommand(args.join(' '), { shell: true, cwd, timeout: 10000 });
	if (!result.stdout.trim()) return [];
	try {
		const parsed = JSON.parse(result.stdout);
		return Array.isArray(parsed) ? parsed : [parsed];
	} catch {
		return [];
	}
}

export async function bdShow(id: string, cwd?: string): Promise<BdIssue> {
	const result = await execaCommand(`bd show ${id} --json`, { shell: true, cwd, timeout: 10000 });
	return JSON.parse(result.stdout);
}

export async function bdCount(filters: { labels?: string[] }, cwd?: string): Promise<number> {
	const args = ['bd', 'count'];
	if (filters.labels?.length) args.push('-l', filters.labels.join(','));
	const result = await execaCommand(args.join(' '), { shell: true, cwd, timeout: 10000 });
	return parseInt(result.stdout.trim(), 10) || 0;
}
