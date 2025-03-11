import { defineStore } from 'pinia';
import { WebSocketStatusEnum, WebSocketBean } from '@bytetrade/core';
import { useAppStore } from 'src/stores/app';
import { useMenuStore } from 'src/stores/menu';
import { useUserStore } from 'src/stores/user';

export interface WebSocketState {
	websocket: WebSocketBean | null;
}

export const useSocketStore = defineStore('websocket', {
	state: () => {
		return {
			websocket: null
		} as WebSocketState;
	},

	actions: {
		start() {
			if (this.isConnecting() || this.isConnected()) {
				console.log(
					'socket Starting..., socket status' + this.websocket?.status
				);
				return;
			}

			if (!this.websocket) {
				let ws_url = process.env.WS_URL || window.location.origin + '/ws';
				console.log(process.env.WS_URL);

				if (ws_url.startsWith('http://')) {
					ws_url = ws_url.substring(7);
					ws_url = 'ws://' + ws_url;
				} else if (ws_url.startsWith('https://')) {
					ws_url = ws_url.substring(8);
					ws_url = 'wss://' + ws_url;
				}
				console.log('ws_url', ws_url);
				if (ws_url === undefined) {
					return;
				}
				const appStore = useAppStore();
				console.log('socket init...', ws_url);

				this.websocket = new WebSocketBean({
					url: ws_url,
					needReconnect: true,
					heartSend: JSON.stringify({
						event: 'ping'
					}),
					heartGet: JSON.stringify({
						event: 'pong'
					}),
					onopen: async () => {
						console.log('socket open ===>');
					},
					onmessage: async (ev) => {
						try {
							const body = JSON.parse(ev.data);
							console.log('socket onmessage body=>', body);
							if (body && body.code === 200) {
								if (body.data) {
									appStore.updateAppStatusBySocket(
										body.data.from,
										body.data.uid,
										body.data.type,
										body.data.status,
										body.data.progress,
										body.data.message
									);
								} else if (body.app && body.type === 'entrance-state-event') {
									appStore.updateAppEntranceBySocket(body.app);
								}
							}
						} catch (e) {
							console.log('message error');
							console.log(e);
						}
					},
					onerror: () => {
						console.log('socket error');
					},
					onreconnect: async () => {
						console.log('socket reconnecting');
					},
					onReconnectSuccess: async () => {
						console.log('socket success reconnect');
						const appStore = useAppStore();
						const menuStore = useMenuStore();
						const userStore = useUserStore();
						await appStore.prefetch();
						await menuStore.init();
						if (!appStore.isPublic) {
							userStore.init();
							appStore.init();
						}
					},
					onReconnectFailure: () => {
						console.log('socket fail reconnect');
					}
				});
			}
			this.websocket.start();
			console.log('socket start !!!!');
		},

		isConnected() {
			if (!this.websocket) {
				return false;
			}
			return this.websocket.status == WebSocketStatusEnum.open;
		},

		isConnecting() {
			if (!this.websocket) {
				return false;
			}
			return this.websocket.status == WebSocketStatusEnum.load;
		},

		isClosed() {
			if (!this.websocket) {
				return true;
			}
			return this.websocket.status == WebSocketStatusEnum.close;
		},

		send(data: any, resend = false) {
			if (!this.websocket) {
				return;
			}
			const sendResult = this.websocket.send(data, resend);
			console.log('== socket send ===>' + sendResult);
			console.log(data);
			console.log('<=====');
			return sendResult;
		},
		close() {
			console.log('socket close');
			if (this.websocket) {
				this.websocket.close();
			}
		},
		dispose() {
			console.log('socket dispose');
			if (this.websocket) {
				this.websocket.dispose();
			}
			this.websocket = null;
		}
	}
});
