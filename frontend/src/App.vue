<template>
	<router-view />
</template>

<script lang="ts">
import { useAppStore } from 'src/stores/app';
import { onBeforeUnmount, onMounted } from 'vue';
import { useSocketStore } from 'src/stores/websocketStore';
import { BtNotify, NotifyDefinedType } from '@bytetrade/ui';
import { bus, BUS_EVENT } from 'src/utils/bus';
import { useUserStore } from 'src/stores/user';
import { useSettingStore } from 'src/stores/setting';
// import { testSatisfies } from 'src/utils/version';

export default {
	async preFetch() {
		const appStore = useAppStore();
		const userStore = useUserStore();
		const settingStore = useSettingStore();
		await appStore.prefetch();
		userStore.init();
		appStore.init();
		settingStore.init();
	},
	setup() {
		const appStore = useAppStore();
		const websocketStore = useSocketStore();

		onMounted(async () => {
			if (!appStore.isPublic) {
				// testSatisfies();
				websocketStore.start();
			}

			bus.on(BUS_EVENT.APP_BACKEND_ERROR, onErrorMessage);
		});

		const onErrorMessage = (failureMessage: string) => {
			BtNotify.show({
				type: NotifyDefinedType.FAILED,
				message: failureMessage
			});
		};

		onBeforeUnmount(() => {
			bus.off(BUS_EVENT.APP_BACKEND_ERROR, onErrorMessage);
		});
	}
};
</script>
