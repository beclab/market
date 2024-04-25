const globalConfig = {
	url:
		process.env.IS_PUBLIC === 'true' ? 'https://app-test.jointerminus.com' : ''
};

export default globalConfig;
