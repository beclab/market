<template>
	<div
		class="app-intro-card-root bg-background-1"
		v-if="contentSlot || description"
	>
		<div class="app-intro-card-title text-subtitle2 text-ink-1">
			{{ title }}
		</div>
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
	padding: 20px;

	.app-intro-card-title {
		margin-bottom: 12px;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 100%;
	}
}
</style>
