<template>
	<div
		v-if="skeleton"
		class="column justify-start items-start"
		style="cursor: pointer"
	>
		<div class="topic-skeleton-square">
			<q-skeleton class="topic-skeleton-item" />
		</div>
		<q-skeleton width="100px" height="12px" style="margin-top: 12px" />
		<q-skeleton width="200px" height="22px" style="margin-top: 2px" />
		<q-skeleton width="180px" height="18px" style="margin-top: 2px" />
	</div>
	<div v-else class="column justify-start items-start" style="cursor: pointer">
		<q-img
			class="topic-item-img"
			:src="item.iconimg ? item.iconimg : '../appIntro.svg'"
			:alt="item.iconimg"
			ratio="1.6"
		>
			<template v-slot:loading>
				<q-skeleton class="topic-item-img" />
			</template>
		</q-img>
		<span class="topic-item-desc">{{ item.des }}</span>
		<span class="topic-item-name">{{ item.name }}</span>
		<span class="topic-item-introduction">{{ item.introduction }}</span>
	</div>
</template>

<script setup lang="ts">
import { PropType } from 'vue';
import { TopicInfo } from 'src/constants/constants';

defineProps({
	item: {
		type: Object as PropType<TopicInfo>,
		require: false
	},
	skeleton: {
		type: Boolean,
		default: false
	}
});
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

.topic-item-desc {
	font-family: Roboto;
	font-size: 10px;
	font-weight: 500;
	line-height: 12px;
	letter-spacing: 0em;
	text-align: left;
	margin-top: 12px;
	height: auto;
	width: auto;
	font-style: normal;
	color: var(--Grey-05, #adadad);
	overflow: hidden;
	text-overflow: ellipsis;
	display: -webkit-box;
	-webkit-line-clamp: 1;
	-webkit-box-orient: vertical;
}

.topic-item-name {
	font-family: Roboto;
	font-size: 16px;
	font-weight: 700;
	line-height: 24px;
	letter-spacing: 0em;
	text-align: left;
	height: auto;
	width: auto;
	font-style: normal;
	color: $title;
	overflow: hidden;
	text-overflow: ellipsis;
	display: -webkit-box;
	-webkit-line-clamp: 1;
	-webkit-box-orient: vertical;
}

.topic-item-introduction {
	font-family: Roboto;
	font-size: 14px;
	font-weight: 400;
	line-height: 20px;
	letter-spacing: 0em;
	text-align: left;
	height: auto;
	width: auto;
	font-style: normal;
	color: var(--Grey-05, #adadad);
	overflow: hidden;
	text-overflow: ellipsis;
	display: -webkit-box;
	-webkit-line-clamp: 1;
	-webkit-box-orient: vertical;
}
</style>
