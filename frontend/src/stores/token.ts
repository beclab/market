import { defineStore } from 'pinia';
import { Token } from '../constants/constants';
import axios from 'axios';

export type RootState = {
	token: Token;
	url: string | null;
};

export const useTokenStore = defineStore('token', {
	state: () => {
		return {
			token: {},
			url: ''
		} as RootState;
	},
	actions: {
		async refresh_token() {
			try {
				const data: any = await axios.post(
					this.url + '/bfl/iam/v1alpha1/refresh-token',
					{
						token: this.token.refresh_token
					}
				);
				console.log(data);
				this.setToken(JSON.parse(data));
			} catch (e) {
				console.log(e);
			}
		},
		setToken(new_token: Token) {
			this.token = new_token;
			if (new_token) {
				localStorage.setItem('token', JSON.stringify(new_token));
			} else {
				localStorage.removeItem('token');
			}
		}
	}
});
