export function getRequireImage(path: string): string {
	if (!path) {
		return '';
	}
	if (path.startsWith('http')) {
		return path;
	}
	// webpack
	// return require(`../assets/${path}`);
	// vite
	return new URL(`../assets/${path}`, import.meta.url).href;
}
