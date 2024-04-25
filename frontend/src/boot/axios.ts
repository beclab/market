import { boot } from 'quasar/wrappers';
import axios, { AxiosInstance, AxiosResponse } from 'axios';
import { useTokenStore } from 'src/stores/token';
import { bus, BUS_EVENT } from 'src/utils/bus';
import { i18n } from 'src/boot/i18n';

declare module '@vue/runtime-core' {
	interface ComponentCustomProperties {
		$axios: AxiosInstance;
	}
}

// Be careful when using SSR for cross-request state pollution
// due to creating a Singleton instance here;
// If any client changes this (global) instance, it might be a
// good idea to move this instance creation inside of the
// "export default () => {}" function below (which runs individually
// for each client)
const api = axios.create({
	withCredentials: true
});
//const notifaction_timeout = 1;

// const whiteList = ['/bfl/iam/v1alpha1/refresh-token']
let callbacks: any[] = [];
let isRefreshing = false;

function addCallbacks(callback: () => void) {
	callbacks.push(callback);
}

function onAccessTokenFetched() {
	callbacks.forEach((callback) => {
		callback();
	});
	callbacks = [];
}

export default boot(({ app }) => {
	// for use inside Vue files (Options API) through this.$axios and this.$api

	app.config.globalProperties.$axios = axios;
	// ^ ^ ^ this will allow you to use this.$axios (for Vue Options API form)
	//       so you won't necessarily have to import axios in each vue file
	app.config.globalProperties.$api = api;

	// app.config.globalProperties.$axios.interceptors.request.use(
	//   async (config: AxiosRequestConfig) => {
	//     const tokenStore = useTokenStore();
	//     // config.headers!['Access-Control-Allow-Origin'] = '*';
	//     // config.headers!['Access-Control-Allow-Headers'] = 'X-Requested-With,Content-Type'
	//     // config.headers!['Access-Control-Allow-Methods'] = 'PUT,POST,GET,DELETE,OPTIONS'

	//     // if (whiteList.some(item => config.url?.includes(item))) {
	//     //   return config
	//     // }
	//     // console.log('getCookie', getCookie('auth_token'))
	//     // const token = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE2OTI4NDkzODQsImlhdCI6MTY5MTYzOTc4NCwiaXNzIjoia3ViZXNwaGVyZSIsInN1YiI6InlvbmdoZW5nMyIsInRva2VuX3R5cGUiOiJhY2Nlc3NfdG9rZW4iLCJ1c2VybmFtZSI6InlvbmdoZW5nMyIsImV4dHJhIjp7InVuaW5pdGlhbGl6ZWQiOlsidHJ1ZSJdfX0.s5R5b798v8f3pWTPuL12J9tQZKp1GC88rKdVusqFLYg'
	//     // config.headers!['X-Authorization'] = token;

	//     if (Cookies.get('auth_token')) {
	//       config.headers!['X-Authorization'] = getCookie('auth_token');
	//       return config;
	//     } else if (tokenStore.$state.token?.access_token) {
	//       // const curTimeSeconds = new Date().getTime() / 1000;
	//       // const expiresAt = tokenStore.$state.token.expires_at;
	//       // if (curTimeSeconds > expiresAt){
	//       //   await tokenStore.refresh_token()
	//       // }
	//       console.log('expiresAt', tokenStore.$state.token.expires_at);
	//       config.headers!['X-Authorization'] =
	//         tokenStore.$state.token?.access_token;
	//       return config;
	//     } else {
	//       return config;
	//     }
	//   }
	// );

	app.config.globalProperties.$axios.interceptors.response.use(
		(response: AxiosResponse) => {
			if (!response || response.status != 200 || !response.data) {
				bus.emit(
					BUS_EVENT.APP_BACKEND_ERROR,
					i18n.global.t('error.network_error')
				);
			}

			const data = response.data;

			if (data.code == 100001) {
				const config = response.config;
				const tokenStore = useTokenStore();

				const retryOriginalRequest = new Promise((resolve) => {
					addCallbacks(() => {
						config.headers['X-Authorization'] =
							tokenStore.$state.token?.access_token;
						resolve(app.config.globalProperties.$axios.request(config));
					});
				});

				if (!isRefreshing) {
					isRefreshing = true;
					tokenStore
						.refresh_token()
						.then(() => {
							onAccessTokenFetched();
						})
						.catch(() => {
							throw Error(data.message);
						})
						.finally(() => {
							isRefreshing = false;
						});
				}
				return retryOriginalRequest;
			}

			if (data.code != 200) {
				// if( data.message ) {
				//   Notify.create({
				//       type: 'negative',
				//       message: '' + data.code +' ' + data.message
				//     })
				// }
				//return response;
				// throw Error(data?.message);
			}

			return data;
		}
	);

	// ^ ^ ^ this will allow you to use this.$api (for Vue Options API form)
	//       so you can easily perform requests against your app's API
});

// function getCookie(cname: string) {
// 	const name = cname + '=';
// 	const nameArray = document.cookie.split(';');
// 	for (let i = 0; i < nameArray.length; i++) {
// 		const c = nameArray[i].trim();
// 		if (c.indexOf(name) == 0) {
// 			return c.substring(name.length, c.length);
// 		}
// 	}
// }

export { api };
