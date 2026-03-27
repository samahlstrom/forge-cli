import { execSync } from 'node:child_process';

export function isGitRepo(cwd: string): boolean {
	try {
		execSync('git rev-parse --is-inside-work-tree', { cwd, stdio: 'pipe' });
		return true;
	} catch {
		return false;
	}
}

export function getMainBranch(cwd: string): string {
	try {
		const result = execSync(
			'git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null || echo refs/heads/main',
			{ cwd, stdio: 'pipe' },
		);
		const ref = result.toString().trim();
		return ref.replace('refs/remotes/origin/', '').replace('refs/heads/', '');
	} catch {
		return 'main';
	}
}

export function getCurrentBranch(cwd: string): string {
	try {
		return execSync('git branch --show-current', { cwd, stdio: 'pipe' })
			.toString()
			.trim();
	} catch {
		return '';
	}
}
