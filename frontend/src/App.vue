<template>
	<router-view v-if="appStore.pageData" />
</template>

<script lang="ts" setup>
import { useAppStore } from 'src/stores/app';
import { onBeforeMount, onBeforeUnmount } from 'vue';
import { useSocketStore } from 'src/stores/websocketStore';
import { BtNotify, NotifyDefinedType } from '@bytetrade/ui';
import { bus, BUS_EVENT } from 'src/utils/bus';
import { useUserStore } from 'src/stores/user';
import { i18n } from 'src/boot/i18n';
import { supportLanguages } from 'src/i18n';
import { useMenuStore } from 'src/stores/menu';
// import { testSatisfies } from 'src/utils/version';
//don't delete follow
//sym_r_radar
//sym_r_business_center
//sym_r_extension
//sym_r_interests
//sym_r_group
//sym_r_featured_play_list
//sym_r_stack
const appStore = useAppStore();
const websocketStore = useSocketStore();
const userStore = useUserStore();
const menuStore = useMenuStore();

let terminusLanguage = '';
let terminusLanguageInfo: any = document.querySelector(
	'meta[name="terminus-language"]'
);
if (terminusLanguageInfo && terminusLanguageInfo.content) {
	terminusLanguage = terminusLanguageInfo.content;
} else {
	terminusLanguage = navigator.language;
}

console.log(navigator.language);

if (terminusLanguage) {
	if (supportLanguages.find((e) => e.value == terminusLanguage)) {
		i18n.global.locale.value = terminusLanguage as any;
	}
}

const initializeApp = async () => {
	await appStore.prefetch();
	await menuStore.init();
	if (!appStore.isPublic) {
		// testSatisfies();
		websocketStore.start();
		userStore.init();
		appStore.init();
	}
};

const onErrorMessage = (failureMessage: string) => {
	BtNotify.show({
		type: NotifyDefinedType.FAILED,
		message: failureMessage
	});
};

const onVisibilityChange = () => {
	if (document.visibilityState === 'visible') {
		console.log('Page is active');
		if (websocketStore.isClosed()) {
			initializeApp().catch((err) => {
				console.error('Error during app re-initialization:', err);
			});
		}
	} else {
		console.log('Page is in background');
	}
};

onBeforeMount(async () => {
	await initializeApp();
	bus.on(BUS_EVENT.APP_BACKEND_ERROR, onErrorMessage);
	document.addEventListener('visibilitychange', onVisibilityChange);
});

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.APP_BACKEND_ERROR, onErrorMessage);
	document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>
