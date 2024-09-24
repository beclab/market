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
import { i18n } from 'src/boot/i18n';
import { supportLanguages } from 'src/i18n';
import { useMenuStore } from 'src/stores/menu';
// import { testSatisfies } from 'src/utils/version';

export default {
	setup() {
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

		onMounted(async () => {
			await appStore.prefetch();
			await menuStore.init();
			if (!appStore.isPublic) {
				// testSatisfies();
				websocketStore.start();
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
