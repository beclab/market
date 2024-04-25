<template>
	<div class="app-intro-card-root" v-if="contentSlot || description">
		<div class="app-intro-card-title">{{ title }}</div>
		<slot v-if="contentSlot" name="content" />
		<expend-text-view :display-line="10" :text="description" />
	</div>
</template>

<script lang="ts" setup>
import { useSlots } from 'vue';
import ExpendTextView from 'components/appintro/ExpendTextView.vue';

defineProps({
	title: {
		type: String,
		default: '',
		require: false
	},
	description: {
		type: String,
		default: '',
		require: false
	}
});

const contentSlot = !!useSlots().content;
</script>

<style scoped lang="scss">
.app-intro-card-root {
	border-radius: 12px;
	width: 100%;
	height: auto;
	background: white;
	padding: 20px;

	.app-intro-card-title {
		font-family: Roboto;
		font-size: 14px;
		font-weight: 500;
		line-height: 20px;
		letter-spacing: 0;
		text-align: left;
		color: $title;
		margin-bottom: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 100%;
	}
}
</style>
