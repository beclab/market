<template>
	<router-view />
</template>

<script lang="ts" setup>
import { useAppStore } from 'src/stores/app';
import { onBeforeUnmount, onMounted } from 'vue';
import { useSocketStore } from 'src/stores/websocketStore';
import { BtNotify, NotifyDefinedType } from '@bytetrade/ui';
import { bus, BUS_EVENT } from 'src/utils/bus';
import { useUserStore } from 'src/stores/user';
import { useSettingStore } from 'src/stores/setting';

const appStore = useAppStore();
const userStore = useUserStore();
const settingStore = useSettingStore();
const websocketStore = useSocketStore();

onMounted(async () => {
	if (!appStore.isPublic) {
		websocketStore.start();
		userStore.init();
		appStore.init();
		settingStore.init();
	}

	bus.on(BUS_EVENT.APP_BACKEND_ERROR, onErrorMessage);
	// await set_nsfw(false)
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
</script>
