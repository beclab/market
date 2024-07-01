<template>
	<page-container :title-height="56">
		<template v-slot:title>
			<title-bar :show="true" @onReturn="router.back()" />
		</template>
		<template v-slot:page>
			<div class="log-scroll">
				<app-store-body title="Logs" :title-separator="true">
					<template v-slot:body>
						<q-table
							v-if="rows.length > 0 || loading"
							:rows="rows"
							flat
							:columns="columns"
							row-key="name + time"
							:loading="loading"
							v-model:pagination="pagination"
							hide-pagination
							style="margin-top: 12px"
						>
							<template v-slot:header="props">
								<q-tr :props="props" style="height: 32px">
									<q-th
										v-for="col in props.cols"
										:key="col.name"
										:props="props"
										class="text-body3 text-grey-5"
									>
										{{ col.label }}
									</q-th>
								</q-tr>
							</template>
							<template v-slot:body-cell-status="props">
								<q-td :props="props">
									<q-badge
										rounded
										:color="
											props.row.status === 'Completed'
												? 'green'
												: props.row.status === 'Failed'
													? 'red'
													: 'grey-5'
										"
										class="q-mr-sm"
									/>
									<span>{{ props.row.status }}</span>
								</q-td>
							</template>
							<template v-slot:body-cell-message="props">
								<q-td :props="props">
									<div class="log-message">
										{{ props.row.message }}
									</div>
								</q-td>
							</template>
						</q-table>

						<div
							v-if="rows.length > 0 || loading"
							class="row justify-center q-mt-md"
						>
							<q-pagination
								v-model="pagination.page"
								color="grey-8"
								input
								:max="pagesNumber"
								size="sm"
							/>
						</div>

						<empty-view v-else :label="t('my.no_logs')" class="empty-view" />
					</template>
				</app-store-body>
			</div>
		</template>
	</page-container>
</template>

<script lang="ts" setup>
import { onMounted, ref, computed } from 'vue';
import { getOperateHistory } from 'src/api/private/user';
import { date } from 'quasar';
import TitleBar from 'components/base/TitleBar.vue';
import { useRoute, useRouter } from 'vue-router';
import PageContainer from 'components/base/PageContainer.vue';
import AppStoreBody from 'src/components/base/AppStoreBody.vue';
import { useI18n } from 'vue-i18n';
import EmptyView from 'src/components/base/EmptyView.vue';
import { capitalizeFirstLetter } from 'src/utils/utils';

const router = useRouter();
const route = useRoute();
const { t } = useI18n();
const type = ref(route.params.type as string);

const columns: any = [
	{
		name: 'createTime',
		align: 'left',
		label: t('base.time'),
		field: 'time',
		format: (time: string) => formattedDate(time)
	},
	{
		name: 'type',
		align: 'left',
		label: t('base.type'),
		field: 'type'
	},
	{
		name: 'name',
		align: 'left',
		label: t('base.name'),
		field: 'name'
	},
	{
		name: 'version',
		align: 'left',
		label: t('detail.chart_version'),
		field: 'version'
	},
	{
		name: 'operations',
		align: 'left',
		label: t('base.operations'),
		field: 'operations'
	},
	{
		name: 'status',
		align: 'left',
		label: t('base.status'),
		field: 'status'
	},
	{
		name: 'message',
		align: 'left',
		label: t('base.message'),
		field: 'message'
	}
];

const formattedDate = (datetime: string) => {
	const originalDate = new Date(datetime);
	return date.formatDate(originalDate, 'YYYY-MM-DD HH:mm:ss');
};

const pagination = ref({
	sortBy: 'desc',
	page: 1,
	rowsPerPage: 10
});

const pagesNumber = computed(() =>
	Math.ceil(rows.value.length / pagination.value.rowsPerPage)
);

const loading = ref(false);
const rows = ref([]);

onMounted(async () => {
	loading.value = true;
	getOperateHistory(type.value)
		.then((res: any) => {
			console.log(res);
			rows.value = res.map((item: any) => ({
				time: item.statusTime,
				name: item.appName,
				type: capitalizeFirstLetter(item.resourceType),
				status: capitalizeFirstLetter(item.status),
				operations: capitalizeFirstLetter(item.opType),
				message: capitalizeFirstLetter(item.message),
				version: item.version
			}));
		})
		.finally(() => {
			loading.value = false;
		});
});
</script>

<style scoped lang="scss">
.log-scroll {
	width: 100%;
	height: calc(100vh - 56px);
	padding: 0 44px;

	.log-message {
		max-width: 500px !important;
		white-space: pre-line;
		overflow: hidden;
		text-overflow: ellipsis;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		-webkit-box-orient: vertical;
	}

	.empty-view {
		width: 100%;
		height: 600px;
	}
}
</style>
