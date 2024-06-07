<template>
	<div
		v-if="skeleton"
		class="column justify-start items-start"
		style="cursor: pointer"
	>
		<div class="topic-skeleton-square">
			<q-skeleton class="topic-skeleton-item" />
		</div>
		<app-small-card :skeleton="true" />
	</div>
	<div v-else class="column justify-start items-start cursor-pointer">
		<q-img
			@click="handleImgClick"
			class="topic-item-img"
			:src="item.iconimg ? item.iconimg : '../appIntro.svg'"
			:alt="item.iconimg"
			ratio="1.6"
		>
			<template v-slot:loading>
				<q-skeleton class="topic-item-img" />
			</template>
		</q-img>
		<app-small-card :item="app" ref="cardRef" :is-last-line="true" />
	</div>
</template>

<script setup lang="ts">
import { onMounted, PropType, ref, watch } from 'vue';
import { AppStoreInfo, TopicInfo } from 'src/constants/constants';
import AppSmallCard from 'src/components/appcard/AppSmallCard.vue';
import { updateAppStoreList } from 'src/utils/bus';

const props = defineProps({
	item: {
		type: Object as PropType<TopicInfo>,
		require: false
	},
	skeleton: {
		type: Boolean,
		default: false
	}
});
const app = ref<AppStoreInfo | null>(null);
const cardRef = ref();

onMounted(() => {
	if (props.item && props.item.apps.length > 0) {
		app.value = props.item.apps[0];
	}
});

const handleImgClick = () => {
	cardRef.value.goAppDetails();
};

watch(
	() => props.item,
	() => {
		if (props.item && props.item.apps && app.value) {
			props.item.apps.forEach((item: AppStoreInfo) => {
				if (app.value) {
					updateAppStoreList([app.value], item);
				}
			});
		}
	},
	{
		deep: true
	}
);
</script>

<style scoped lang="scss">
.topic-skeleton-square {
	width: 100%;
	padding-top: 62.5%;
	position: relative;

	.topic-skeleton-item {
		position: absolute;
		border-radius: 12px;
		top: 0;
		width: 100%;
		height: 100%;
	}
}

.topic-item-img {
	border-radius: 12px;
	width: 100%;
	height: 100%;
}
</style>
