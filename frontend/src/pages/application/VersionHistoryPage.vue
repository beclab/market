<template>
	<page-container v-model="showShadow" :title-height="56">
		<template v-slot:title>
			<title-bar
				:show="true"
				:show-title="showShadow"
				:title="i18n.t('version_history')"
				@onReturn="router.back()"
				:shadow="showShadow"
			/>
		</template>
		<template v-slot:page>
			<div class="version-history-scroll">
				<app-store-body
					:label="i18n.t('version_history')"
					:loading="loading"
					:title-separator="true"
				>
					<template v-slot:loading>
						<template v-for="item in 20" :key="item">
							<version-record-item :skeleton="true" />
						</template>
					</template>
					<template v-slot:body>
						<empty-view
							v-if="isEmpty"
							:label="i18n.t('no_version_history_desc')"
							class="empty_view"
						/>
						<div v-else>
							<template v-for="item in versionList" :key="item.mergedAt">
								<version-record-item :record="item" />
							</template>
						</div>
					</template>
				</app-store-body>
			</div>
		</template>
	</page-container>
</template>

<script lang="ts" setup>
import PageContainer from 'components/base/PageContainer.vue';
import EmptyView from 'components/base/EmptyView.vue';
import { useRoute, useRouter } from 'vue-router';
import { onMounted, ref } from 'vue';
import VersionRecordItem from 'components/appcard/VersionRecordItem.vue';
import { getVersionHistory } from 'src/api/storeApi';
import { VersionRecord } from 'src/constants/constants';
import TitleBar from 'components/base/TitleBar.vue';
import AppStoreBody from 'components/base/AppStoreBody.vue';
import { useI18n } from 'vue-i18n';

const i18n = useI18n();
const router = useRouter();
const route = useRoute();
const loading = ref(false);
const isEmpty = ref(false);
const showShadow = ref(false);
const versionList = ref<VersionRecord[]>([]);
const appName = route.params.name as string;

onMounted(async () => {
	if (appName) {
		loading.value = true;
		versionList.value = await getVersionHistory(appName);
		isEmpty.value = versionList.value.length === 0;
	}
	loading.value = false;
});
</script>

<style scoped lang="scss">
.version-history-scroll {
	width: 100%;
	height: calc(100vh - 56px);
	padding: 0 44px;

	.empty_view {
		width: 100%;
		height: 500px;
	}
}
</style>
