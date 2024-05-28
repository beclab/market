<template>
	<div v-if="appList" :class="rule">
		<template v-for="item in showAppList" :key="item.name">
			<slot name="card" :item="item" />
		</template>
		<app-card-hide-border />
	</div>
	<div v-else :class="rule">
		<template v-for="index in showAppSize" :key="index">
			<slot name="card" />
		</template>
		<app-card-hide-border />
	</div>
</template>

<script lang="ts" setup>
import AppCardHideBorder from 'components/appcard/AppCardHideBorder.vue';
import { onBeforeUnmount, onMounted, ref } from 'vue';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';
import { AppStoreInfo } from 'src/constants/constants';
import { getSliceArray } from 'src/utils/utils';
import { useQuasar } from 'quasar';

const props = defineProps({
	rule: {
		type: String,
		require: true
	},
	appList: {
		type: Object
	},
	showSize: {
		type: String,
		default: '15,9,6'
	}
});

const $q = useQuasar();
const allAppList = ref();
const showAppList = ref();
const sizeArray = props.showSize.split(',');
const showAppSize = ref(
	$q.screen.lg || $q.screen.xl
		? Number(sizeArray[0])
		: $q.screen.md
			? Number(sizeArray[1])
			: Number(sizeArray[2])
);
let resizeTimer: NodeJS.Timeout | null = null;

onMounted(async () => {
	if (props.appList) {
		allAppList.value = props.appList;
		updateAppList();
		bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateAppData);
	}
	window.addEventListener('resize', () => {
		updateAppList();
	});
});

onBeforeUnmount(() => {
	if (props.appList) {
		bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateAppData);
	}
	window.removeEventListener('resize', () => {
		updateAppList();
	});
});

const updateAppList = () => {
	showAppSize.value =
		$q.screen.lg || $q.screen.xl
			? Number(sizeArray[0])
			: $q.screen.md
				? Number(sizeArray[1])
				: Number(sizeArray[2]);
	if (props.appList) {
		showAppList.value = getSliceArray(allAppList.value, showAppSize.value);
	}
};

const updateAppData = (app: AppStoreInfo) => {
	resizeTimer = setTimeout(function () {
		console.log(`update status ${app.name}`);
		updateAppStoreList(allAppList.value, app);
		updateAppStoreList(showAppList.value, app);
	}, 400);
};
</script>

<style scoped lang="scss"></style>
