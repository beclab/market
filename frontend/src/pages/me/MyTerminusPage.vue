<template>
	<page-container>
		<template v-slot:page>
			<div class="my-page-scroll">
				<app-store-body
					:title="t('main.my_terminus')"
					:title-separator="true"
				/>

				<div class="tab-parent row justify-between items-center">
					<q-tabs
						class="my-page-tabs"
						v-model="selectedTab"
						dense
						outside-arrows
						active-color="primary"
						indicator-color="transparent"
						align="left"
						:narrow-indicator="false"
					>
						<template v-for="(item, index) in tabArray" :key="`al` + index">
							<q-tab style="padding: 0" :name="item">
								<template v-slot:default>
									<bt-tab-item
										:selected="item === selectedTab"
										:label="item === 'Market' ? t('my.market') : t('my.custom')"
									/>
								</template>
							</q-tab>
						</template>
					</q-tabs>

					<div class="row justify-end items-center">
						<bt-label
							v-if="selectedTab === 'Market'"
							name="sym_r_upload"
							@click="goUpdatePage"
							:label="t('my.available_updates')"
						/>
						<bt-upload-chart v-else>
							<bt-label
								name="sym_r_upload_file"
								:label="t('my.upload_custom_chart')"
							/>
						</bt-upload-chart>
						<q-separator class="column-line" />
						<bt-label
							name="sym_r_assignment"
							:label="t('my.logs')"
							@click="goLogPage"
						/>
					</div>
				</div>

				<q-tab-panels
					v-model="selectedTab"
					animated
					class="my-page-panels"
					keep-alive
				>
					<q-tab-panel name="Market" class="my-page-panel">
						<installed-page />
					</q-tab-panel>
					<q-tab-panel name="Custom" class="my-page-panel">
						<developer-page />
					</q-tab-panel>
				</q-tab-panels>
			</div>
		</template>
	</page-container>
</template>

<script lang="ts" setup>
import PageContainer from 'components/base/PageContainer.vue';
import AppStoreBody from 'components/base/AppStoreBody.vue';
import { onMounted, ref } from 'vue';
import InstalledPage from 'pages/me/InstalledPage.vue';
import DeveloperPage from 'pages/me/DeveloperPage.vue';
import BtTabItem from 'components/base/BtTabItem.vue';
import BtLabel from 'components/base/BtLabel.vue';
import BtUploadChart from 'components/base/BtUploadChart.vue';
import { useRouter } from 'vue-router';
import { useI18n } from 'vue-i18n';
import { useAppStore } from 'src/stores/app';
import { TRANSACTION_PAGE } from 'src/constants/constants';

const tabArray = ref(['Market', 'Custom']);
const selectedTab = ref('Market');
const router = useRouter();
const { t } = useI18n();
const appStore = useAppStore();

onMounted(async () => {
	appStore.loadApps();
});

const goLogPage = () => {
	if (selectedTab.value === 'Market') {
		router.push({
			name: TRANSACTION_PAGE.Log,
			params: {
				type: 'market'
			}
		});
	} else {
		router.push({
			name: TRANSACTION_PAGE.Log,
			params: {
				type: 'custom'
			}
		});
	}
};

const goUpdatePage = () => {
	router.push({
		name: TRANSACTION_PAGE.Update
	});
};
</script>

<style scoped lang="scss">
.my-page-scroll {
	width: 100%;
	height: 100%;
	padding: 56px 44px 0;

	.tab-parent {
		width: 100%;
		height: 80px;

		.column-line {
			height: 20px;
			width: 1px;
			margin-left: 12px;
			margin-right: 12px;
		}
	}

	.my-page-tabs {
		border-radius: 8px;
		border: 1px solid $separator;
	}

	.my-page-panels {
		width: 100%;
		height: calc(100vh - 56px - 84px - 52px);

		.my-page-panel {
			width: 100%;
			height: 100%;
			padding: 0;
		}
	}
}

.q-tabs--dense .q-tab {
	min-height: 32px;
}
</style>
