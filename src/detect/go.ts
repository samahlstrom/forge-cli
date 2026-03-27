import { join } from 'node:path';
import { exists, readText } from '../utils/fs.js';

type PartialStack = Omit<import('./index.js').DetectedStack, 'features'>;

export async function detectGo(cwd: string): Promise<PartialStack | null> {
	const goModPath = join(cwd, 'go.mod');
	if (!(await exists(goModPath))) return null;

	const goMod = await readText(goModPath);

	const result: PartialStack = {
		language: 'go',
		framework: null,
		preset: 'go',
		testRunner: { name: 'go test', command: 'go test ./...' },
		linter: null,
		typeChecker: { name: 'go vet', command: 'go vet ./...' },
		formatter: { name: 'gofmt', command: 'gofmt -w .' },
		packageManager: 'go modules',
	};

	// Framework detection from go.mod requires
	if (goMod.includes('github.com/gin-gonic/gin')) {
		result.framework = 'gin';
	} else if (goMod.includes('github.com/go-chi/chi')) {
		result.framework = 'chi';
	} else if (goMod.includes('github.com/gofiber/fiber')) {
		result.framework = 'fiber';
	} else if (goMod.includes('github.com/labstack/echo')) {
		result.framework = 'echo';
	}

	// Linter — check if golangci-lint config exists
	if (
		(await exists(join(cwd, '.golangci.yml'))) ||
		(await exists(join(cwd, '.golangci.yaml')))
	) {
		result.linter = { name: 'golangci-lint', command: 'golangci-lint run' };
	}

	return result;
}
