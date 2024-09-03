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
// import { testSatisfies } from 'src/utils/version';

export default {
	setup() {
		const appStore = useAppStore();
		const websocketStore = useSocketStore();
		const userStore = useUserStore();

		onMounted(async () => {
			if (!appStore.isPublic) {
				// testSatisfies();
				websocketStore.start();
				await appStore.prefetch();
				userStore.init();
				appStore.init();
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
