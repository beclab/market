<template>
	<div class="permission-request-root column justify-center">
		<div class="row justify-start items-center">
			<q-icon size="20px" color="ink-2" :name="name" />
			<div class="text-ink-2 text-subtitle2 q-ml-xs">{{ title }}</div>
		</div>
		<div
			class="row justify-start q-mt-md"
			v-for="(item, num) in nodes"
			:key="num"
		>
			<div class="permission-circle-parent">
				<div class="permission-circle bg-ink-2" />
			</div>
			<div class="column justify-start" style="width: calc(100% - 20px)">
				<div class="text-ink-2 text-body2">{{ item.label }}</div>
				<template v-for="(child, index) in item.children" :key="index">
					<div class="text-overline text-ink-3">{{ child.label }}</div>
				</template>
			</div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { PropType } from 'vue';
import { PermissionNode } from '/@/constants/constants';

defineProps({
	name: String,
	title: String,
	nodes: Object as PropType<PermissionNode[]>
});
</script>

<style scoped lang="scss">
.permission-request-root {
	width: 100%;
	height: auto;

	.permission-circle-parent {
		width: 20px;
		height: 20px;
		padding: 7px;

		.permission-circle {
			width: 6px;
			height: 6px;
			border-radius: 50%;
		}
	}
}
</style>
