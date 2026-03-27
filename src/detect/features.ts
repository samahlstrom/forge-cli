import { join } from 'node:path';
import { exists } from '../utils/fs.js';

export interface FeatureFlags {
	git: boolean;
	ci: 'github-actions' | 'gitlab-ci' | 'jenkins' | null;
	docker: boolean;
	playwright: boolean;
	semgrep: boolean;
	firebase: boolean;
	vercel: boolean;
}

export async function detectFeatures(cwd: string): Promise<FeatureFlags> {
	const [git, githubActions, gitlabCi, jenkinsfile, dockerfile, dockerCompose, playwrightConfig, semgrepConfig, firebase, vercel] =
		await Promise.all([
			exists(join(cwd, '.git')),
			exists(join(cwd, '.github', 'workflows')),
			exists(join(cwd, '.gitlab-ci.yml')),
			exists(join(cwd, 'Jenkinsfile')),
			exists(join(cwd, 'Dockerfile')),
			exists(join(cwd, 'docker-compose.yml')),
			exists(join(cwd, 'playwright.config.ts')).then(
				(e) => e || exists(join(cwd, 'playwright.config.js')),
			),
			exists(join(cwd, '.semgrep.yml')),
			exists(join(cwd, 'firebase.json')),
			exists(join(cwd, 'vercel.json')),
		]);

	let ci: FeatureFlags['ci'] = null;
	if (githubActions) ci = 'github-actions';
	else if (gitlabCi) ci = 'gitlab-ci';
	else if (jenkinsfile) ci = 'jenkins';

	return {
		git,
		ci,
		docker: dockerfile || dockerCompose,
		playwright: playwrightConfig,
		semgrep: semgrepConfig,
		firebase,
		vercel,
	};
}
