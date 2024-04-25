<template>
	<div class="empty_root column justify-center items-center">
		<q-img class="empty-image" :src="getRequireImage('search_empty.svg')" />
		<div v-if="label" class="empty-label">{{ label }}</div>
	</div>
</template>

<script lang="ts" setup>
import { getRequireImage } from 'src/utils/imageUtils';

defineProps({
	label: {
		type: String,
		default: ''
	}
});
</script>

<style lang="scss" scoped>
.empty_root {
	background: white;

	.empty-image {
		width: 221.76px;
		height: 153.71px;
	}

	.empty-label {
		margin-top: 50px;
		font-family: Roboto;
		font-size: 14px;
		font-weight: 500;
		line-height: 20px;
		letter-spacing: 0;
		text-align: center;
		color: var(--Grey-08, #5c5551);
	}
}
</style>
