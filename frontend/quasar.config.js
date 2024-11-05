/* eslint-env node */

/*
 * This file runs in a Node context (it's NOT transpiled by Babel), so use only
 * the ES6 features that are supported by your Node version. https://node.green/
 */

// Configuration for your app
// https://v2.quasar.dev/quasar-cli-vite/quasar-config-js

const { configure } = require('quasar/wrappers');
const dotenv = require('dotenv');
const CssMinimizerPlugin = require('css-minimizer-webpack-plugin');
const TerserPlugin = require('terser-webpack-plugin');

dotenv.config();

const path = require('path');

module.exports = configure(function (/* ctx */) {
	return {
		// https://v2.quasar.dev/quasar-cli-webpack/supporting-ts
		supportTS: {
			tsCheckerConfig: {
				eslint: {
					enabled: true,
					files: './src/**/*.{ts,tsx,js,jsx,vue}'
				}
			}
		},

		// https://v2.quasar.dev/quasar-cli-vite/prefetch-feature
		preFetch: true,

		// app boot file (/src/boot)
		// --> boot files are part of "main.js"
		// https://v2.quasar.dev/quasar-cli-vite/boot-files
		boot: ['i18n', 'axios', 'smartEnginEntrance.js', 'bytetrade-ui'],

		// https://v2.quasar.dev/quasar-cli-vite/quasar-config-js#css
		css: ['app.scss', 'iconFont.scss'],

		// https://github.com/quasarframework/quasar/tree/dev/extras
		extras: [
			// 'ionicons-v4',
			// 'mdi-v5',
			// 'fontawesome-v6',
			// 'eva-icons',
			// 'themify',
			// 'line-awesome',
			// 'roboto-font-latin-ext', // this or either 'roboto-font', NEVER both!
			'material-icons',
			'roboto-font', // optional, you are not bound to it
			'bootstrap-icons',
			'themify'
		],

		// Full list of options: https://v2.quasar.dev/quasar-cli-vite/quasar-config-js#build
		build: {
			target: {
				browser: ['es2019', 'edge88', 'firefox78', 'chrome87', 'safari13.1'],
				node: 'node16'
			},
			logLevel: false,
			env: {
				URL: process.env.URL,
				PUBLIC_URL: process.env.PUBLIC_URL,
				WS_URL: process.env.WS_URL,
				LOGIN_USERNAME: process.env.LOGIN_USERNAME,
				LOGIN_PASSWORD: process.env.LOGIN_PASSWORD
			},
			// analyze: true,

			vueRouterMode: 'history', // available values: 'hash', 'history'
			extractCSS: true,
			gzip: true,
			sourcemap: false,
			minify: true,
			// vueRouterBase,
			// vueDevtools,
			// vueOptionsAPI: false,

			// rebuildCache: true, // rebuilds Vite/linter/etc cache on startup

			// publicPath: '/',
			// analyze: true,
			// env: {},
			// rawDefine: {}
			// ignorePublicFolder: true,
			// minify: false,
			// polyfillModulePreload: true,
			// distDir

			// extendViteConf (viteConf) {},
			// viteVuePluginOptions: {},
			chainWebpack(chain, { isClient, isServer }) {
				chain.resolve.alias
					.set('assets', path.resolve('src/assets'))
					.set('statics', path.resolve('src/statics'))
					.set('components', path.resolve('src/components'));
				if (isClient) {
					chain.plugin('optimize-css').use(CssMinimizerPlugin, [
						{
							minimizerOptions: {
								preset: [
									'default',
									{
										mergeLonghand: false,
										cssDeclarationSorter: false
									}
								]
							}
						}
					]);
				}
				// chain.plugin('terser').use(TerserPlugin, [
				// 	{
				// 		terserOptions: {
				// 			compress: {
				// 				drop_console: true,
				// 				pure_funcs: ['console.log']
				// 			}
				// 		}
				// 	}
				// ]);
				chain.optimization.splitChunks({
					chunks: 'all', // The type of chunk that requires code segmentation
					minSize: 20000, // Minimum split file size
					minRemainingSize: 0, // Minimum remaining file size after segmentation
					minChunks: 1, // The number of times it has been referenced before it is split
					maxAsyncRequests: 30, // Maximum number of asynchronous requests
					maxInitialRequests: 30, // Maximum number of initialization requests
					enforceSizeThreshold: 50000,
					cacheGroups: {
						// Cache Group configuration
						defaultVendors: {
							test: /[\\/]node_modules[\\/]/,
							priority: -10,
							reuseExistingChunk: true
						},
						default: {
							minChunks: 2,
							priority: -20,
							reuseExistingChunk: true // Reuse the chunk that has been split
						}
					}
				});
			}
		},
		devServer: {
			https: true,
			host: process.env.PUBLIC_URL
				? 'localhost'
				: process.env.DEV_DOMAIN || 'localhost',
			open: true, // opens browser window automatically,
			port: 8080,
			proxy: process.env.PUBLIC_URL
				? {
						'/app-store': {
							target: process.env.PUBLIC_URL,
							changeOrigin: true
						}
					}
				: {
						'/app-store': {
							target: `https://market.${process.env.ACCOUNT}.myterminus.com`,
							changeOrigin: true
						}
					}
		},

		// https://v2.quasar.dev/quasar-cli-vite/quasar-config-js#framework
		framework: {
			config: {
				dark: 'auto'
			},

			// iconSet: 'material-icons', // Quasar icon set
			// lang: 'en-US', // Quasar language pack

			// For special cases outside of where the auto-import strategy can have an impact
			// (like functional components as one of the examples),
			// you can manually specify Quasar components/directives to be available everywhere:
			//
			// components: [],
			// directives: [],

			// Quasar plugins
			plugins: ['Notify', 'Loading', 'Dialog', 'Cookies']
		},

		// animations: 'all', // --- includes all animations
		// https://v2.quasar.dev/options/animations
		animations: [],

		// https://v2.quasar.dev/quasar-cli-vite/quasar-config-js#sourcefiles
		// sourceFiles: {
		//   rootComponent: 'src/App.vue',
		//   router: 'src/router/index',
		//   store: 'src/store/index',
		//   registerServiceWorker: 'src-pwa/register-service-worker',
		//   serviceWorker: 'src-pwa/custom-service-worker',
		//   pwaManifestFile: 'src-pwa/manifest.json',
		//   electronMain: 'src-electron/electron-main',
		//   electronPreload: 'src-electron/electron-preload'
		// },

		// https://v2.quasar.dev/quasar-cli-vite/developing-pwa/configuring-pwa
		pwa: {
			workboxMode: 'generateSW', // or 'injectManifest'
			injectPwaMetaTags: true,
			swFilename: 'sw.js',
			manifestFilename: 'manifest.json',
			useCredentialsForManifestTag: false
			// useFilenameHashes: true,
			// extendGenerateSWOptions (cfg) {}
			// extendInjectManifestOptions (cfg) {},
			// extendManifestJson (json) {}
			// extendPWACustomSWConf (esbuildConf) {}
		},

		// Full list of options: https://v2.quasar.dev/quasar-cli-vite/developing-cordova-apps/configuring-cordova
		cordova: {
			// noIosLegacyBuildFlag: true, // uncomment only if you know what you are doing
		},

		// Full list of options: https://v2.quasar.dev/quasar-cli-vite/developing-capacitor-apps/configuring-capacitor
		capacitor: {
			hideSplashscreen: true
		},

		// Full list of options: https://v2.quasar.dev/quasar-cli-vite/developing-electron-apps/configuring-electron
		electron: {
			// extendElectronMainConf (esbuildConf)
			// extendElectronPreloadConf (esbuildConf)

			inspectPort: 5858,

			bundler: 'packager', // 'packager' or 'builder'

			packager: {
				// https://github.com/electron-userland/electron-packager/blob/master/docs/api.md#options
				// OS X / Mac App Store
				// appBundleId: '',
				// appCategoryType: '',
				// osxSign: '',
				// protocol: 'myapp://path',
				// Windows only
				// win32metadata: { ... }
			},

			builder: {
				// https://www.electron.build/configuration/configuration

				appId: 'launcher'
			}
		},

		// Full list of options: https://v2.quasar.dev/quasar-cli-vite/developing-browser-extensions/configuring-bex
		bex: {
			contentScripts: ['my-content-script']

			// extendBexScriptsConf (esbuildConf) {}
			// extendBexManifestJson (json) {}
		}
	};
});
