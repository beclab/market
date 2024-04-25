<template>
	<div class="permission-request-root column justify-center">
		<div class="row justify-start items-center">
			<q-icon size="20px" :name="name" />
			<div class="permission-title">{{ title }}</div>
		</div>
		<div
			class="row justify-start q-mt-md"
			v-for="(item, num) in nodes"
			:key="num"
		>
			<div class="permission-circle-parent">
				<div class="permission-circle" />
			</div>
			<div class="column justify-start" style="width: calc(100% - 20px)">
				<div class="permission-label">{{ item.label }}</div>
				<template v-for="(child, index) in item.children" :key="index">
					<div class="permission-desc">{{ child.label }}</div>
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

	.permission-title {
		margin-left: 4px;
		font-family: Roboto;
		font-size: 14px;
		font-weight: 500;
		line-height: 20px;
		letter-spacing: 0em;
		text-align: left;
		color: var(--Grey-08, #5c5551);
	}

	.permission-circle-parent {
		width: 20px;
		height: 20px;
		padding: 7px;

		.permission-circle {
			width: 6px;
			height: 6px;
			background: #5c5551;
			border-radius: 50%;
		}
	}

	.permission-label {
		font-family: Roboto;
		font-size: 14px;
		font-weight: 400;
		line-height: 20px;
		letter-spacing: 0em;
		text-align: left;
		color: var(--Grey-08, #5c5551);
	}

	.permission-desc {
		font-family: Roboto;
		font-size: 10px;
		font-weight: 400;
		line-height: 12px;
		letter-spacing: 0em;
		text-align: left;
		color: var(--Grey-05, #adadad);
	}

	.desc-circle-parent {
		width: 12px;
		height: 12px;
		padding: 5px;

		.desc-circle {
			width: 2px;
			height: 2px;
			background: $grey-5;
			border-radius: 50%;
		}
	}
}
</style>
