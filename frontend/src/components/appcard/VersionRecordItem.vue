<template>
	<div v-if="skeleton" class="version-record-root column justify-start">
		<div class="row justify-between items-center">
			<q-skeleton width="50px" height="20px" />
			<q-skeleton width="40px" height="20px" />
		</div>
		<q-skeleton class="version-record-desc" width="200px" height="20px" />
		<q-skeleton style="margin-top: 2px" width="300px" height="20px" />
	</div>
	<div v-else class="version-record-root column justify-start">
		<div class="row justify-between items-center">
			<div class="version-record-version">{{ record.version }}</div>
			<div class="version-record-time">
				{{ formatTimeDifference(record.mergedAt) }}
			</div>
		</div>

		<expend-text-view
			:display-line="2"
			class="version-record-desc"
			:text="record.upgradeDescription"
		/>
	</div>
</template>

<script lang="ts" setup>
import { PropType } from 'vue';
import { VersionRecord } from 'src/constants/constants';
import ExpendTextView from 'components/appintro/ExpendTextView.vue';
import { formatTimeDifference } from 'src/utils/utils';

defineProps({
	record: {
		type: Object as PropType<VersionRecord>,
		required: true
	},
	skeleton: {
		type: Boolean,
		default: false
	}
});
</script>

<style scoped lang="scss">
.version-record-root {
	width: 100%;
	height: auto;
	padding-top: 20px;
	padding-bottom: 20px;
	border-bottom: 1px solid #ebebeb;

	.version-record-version {
		font-family: Roboto;
		font-size: 16px;
		font-weight: 700;
		line-height: 24px;
		letter-spacing: 0em;
		text-align: left;
		color: var(--Grey-10, #1f1814);
	}

	.version-record-time {
		font-family: Roboto;
		font-size: 14px;
		font-weight: 400;
		line-height: 20px;
		letter-spacing: 0em;
		text-align: right;
		color: var(--Grey-05, #adadad);
	}

	.version-record-desc {
		margin-top: 12px;
	}
}
</style>
