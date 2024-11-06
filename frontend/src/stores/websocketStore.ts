import { defineStore } from 'pinia';
import { WebSocketStatusEnum, WebSocketBean } from '@bytetrade/core';
import { useAppStore } from 'src/stores/app';

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

			this.websocket = new WebSocketBean({
				url: ws_url,
				needReconnect: true,
				reconnectMaxNum: 5,
				reconnectGapTime: 3000,
				heartSend: JSON.stringify({
					event: 'ping',
					data: {}
				}),
				onopen: async () => {
					console.log('websocket open ===>');
				},
				onmessage: async (ev) => {
					try {
						const body = JSON.parse(ev.data);
						console.log('onmessage body=>', body);
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
				onreconnect: () => {
					console.log('socket start reconnect');
				},
				onFailReconnect: () => {
					console.log('socket fail reconnect');
				}
			});
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

		send(data: any, resend = false) {
			if (!this.websocket) {
				return;
			}
			const sendResult = this.websocket!.send(data, resend);
			console.log('== send ===>' + sendResult);
			console.log(data);
			console.log('<=====');
			return sendResult;
		},
		restart() {
			console.log('restart websocket');
			if (this.websocket) {
				this.websocket!.dispose();
			}
			this.start();
		},
		dispose() {
			console.log('dispose');
			if (this.websocket) {
				this.websocket!.dispose();
			}
			this.websocket = null;
		}
	}
});
