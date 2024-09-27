<template>
	<div v-if="skeleton" class="column justify-start items-start">
		<div class="topic-skeleton-square">
			<q-skeleton class="topic-skeleton-item" />
		</div>
		<q-skeleton width="100px" height="12px" style="margin-top: 12px" />
		<q-skeleton width="200px" height="22px" style="margin-top: 2px" />
		<q-skeleton width="180px" height="18px" style="margin-top: 2px" />
	</div>
	<div v-else class="column justify-start items-start">
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
		<span class="topic-item-overflow text-ink-3 text-caption q-mt-md">{{
			getTempI18nValue(item.des)
		}}</span>
		<span class="topic-item-overflow text-ink-1 text-h6">{{
			getTempI18nValue(item.name)
		}}</span>
		<span class="topic-item-overflow text-ink-3 text-body2">{{
			getTempI18nValue(item.introduction)
		}}</span>
	</div>
</template>

<script setup lang="ts">
import { PropType } from 'vue';
import { getTempI18nValue, TopicInfo } from 'src/constants/constants';

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

.topic-item-overflow {
	text-align: left;
	height: auto;
	width: auto;
	font-style: normal;
	overflow: hidden;
	text-overflow: ellipsis;
	display: -webkit-box;
	-webkit-line-clamp: 1;
	-webkit-box-orient: vertical;
}
</style>
