<template>
	<div
		class="slip_layer_level_item column justify-center items-center"
		:style="{ '--line': last ? 'none' : '1px solid #EBEBEB' }"
	>
		<div class="slip_layer_level_name text-color-prompt-message">
			{{ name }}
		</div>
		<q-icon
			v-if="src"
			size="24px"
			class="slip_layer_level_icon"
			:src="imageRef"
			:name="src"
		/>
		<div v-else class="row justify-center items-center">
			<div class="slip_layer_level_number text-color-subTitle">{{ data }}</div>
		</div>
		<div class="slip_layer_level_unit text-color-subTitle">
			{{ unit ? unit : '-' }}
		</div>
	</div>
</template>

<script lang="ts" setup>
import { onMounted, ref } from 'vue';
import { getRequireImage } from 'src/utils/imageUtils';

const props = defineProps({
	name: {
		type: String,
		default: '',
		require: true
	},
	src: String,
	data: {
		type: String,
		default: '-'
	},
	unit: {
		type: String,
		default: '-'
	},
	last: {
		type: Boolean,
		default: false
	}
});

const imageRef = ref();

onMounted(() => {
	if (props.src) {
		imageRef.value = getRequireImage(props.src);
	}
});
</script>

<style scoped lang="scss">
.slip_layer_level_item {
	height: 72px;
	width: 100%;
	align-items: center;
	border-right: var(--line);

	.slip_layer_level_name {
		font-family: Roboto;
		font-size: 12px;
		font-weight: 400;
		line-height: 16px;
		letter-spacing: 0em;
		text-align: center;
		width: 100%;
	}

	.slip_layer_level_icon {
		margin-top: 8px;
		margin-bottom: 8px;
	}

	.slip_layer_level_number {
		height: 24px;
		margin-top: 8px;
		margin-bottom: 8px;
		font-family: Roboto;
		font-size: 16px;
		font-weight: 500;
		line-height: 24px;
		letter-spacing: 0em;
		text-align: center;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.slip_layer_level_unit {
		font-family: Roboto;
		font-size: 12px;
		font-weight: 400;
		line-height: 16px;
		letter-spacing: 0em;
		text-align: center;
		width: 100%;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
}
</style>
