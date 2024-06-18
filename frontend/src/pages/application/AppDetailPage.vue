<template>
	<div class="app-detail-root">
		<app-title-bar
			:item="item"
			:show-icon="showIcon(cfgType)"
			:show-header-bar="showHeaderBar"
			:show-install-btn="!appStore.isPublic"
			:absolute="true"
		/>
		<page-container
			:vertical-position="100"
			v-model="showHeaderBar"
			:is-app-details="true"
		>
			<template v-slot:page>
				<div class="app-detail-page column">
					<div
						class="full-width column"
						:class="
							$q.dark.isActive ? 'app-detail-top-dark' : 'app-detail-top-light'
						"
					>
						<title-bar
							:show="true"
							:show-back="true"
							@on-return="clickReturn"
						/>
						<div
							v-if="!item"
							class="app-details-padding"
							:style="showIcon(cfgType) ? '' : 'margin-bottom:20px'"
						>
							<app-card
								:type="cfgType"
								:skeleton="true"
								:larger="true"
								:is-last-line="true"
							/>
						</div>
						<div
							v-else
							class="app-details-padding"
							:style="showIcon(cfgType) ? '' : 'margin-bottom:20px'"
						>
							<app-card
								:type="cfgType"
								:item="item"
								:development="false"
								:larger="true"
								:is-last-line="true"
							/>
						</div>
						<div
							v-if="
								item &&
								item.preflightError &&
								item.status === APP_STATUS.preflightFailed &&
								!appStore.isPublic
							"
							class="app-details-padding app-detail-warning bg-red-soft row justify-start"
						>
							<q-icon color="negative" size="16px" name="sym_r_error" />
							<div class="column justify-start">
								<div class="text-body-3 text-negative q-ml-sm">
									{{ t('unable_to_install_app') }}
								</div>
								<template v-for="message in item.preflightError" :key="message">
									<div class="text-body-3 text-negative">
										{{ `&nbsp;·&nbsp;${message}` }}
									</div>
								</template>
							</div>
						</div>

						<div class="app-detail-line" />
						<div
							class="app-details-padding app-detail-install-configuration row justify-start items-center"
							:style="{
								'--showSize':
									item &&
									item.options &&
									item.options.appScope &&
									item.options.appScope.clusterScoped
										? '7'
										: '6'
							}"
						>
							<install-configuration
								v-if="
									item &&
									item.options &&
									item.options.appScope &&
									item.options.appScope.clusterScoped
								"
								:name="t('base.scope')"
								src="sym_r_lan"
								:unit="t('base.cluster_app')"
							/>
							<install-configuration
								:name="t('base.developer')"
								src="sym_r_group"
								:unit="item && item.developer"
							/>
							<install-configuration
								:name="t('base.language')"
								:data="language.toUpperCase()"
								:unit="
									languageLength > 0
										? `+ ${languageLength} more`
										: convertLanguageCodeToName(language)
								"
							/>
							<install-configuration
								:name="t('detail.require_memory')"
								:data="
									item && item.requiredMemory
										? getValueByUnit(
												item.requiredMemory,
												getSuitableUnit(item.requiredMemory, 'memory')
											)
										: '-'
								"
								:unit="
									item && item.requiredMemory
										? getSuitableUnit(item.requiredMemory, 'memory')
										: '-'
								"
							/>
							<install-configuration
								:name="t('detail.require_disk')"
								:data="
									item && item.requiredDisk
										? getValueByUnit(
												item.requiredDisk,
												getSuitableUnit(item.requiredDisk, 'disk')
											)
										: '-'
								"
								:unit="
									item && item.requiredDisk
										? getSuitableUnit(item.requiredDisk, 'disk')
										: '-'
								"
							/>
							<install-configuration
								:name="t('detail.require_cpu')"
								:data="
									item && item.requiredCpu
										? getValueByUnit(
												item.requiredCpu,
												getSuitableUnit(item.requiredCpu, 'cpu')
											)
										: '-'
								"
								:unit="
									item && item.requiredCpu
										? getSuitableUnit(item.requiredCpu, 'cpu')
										: '-'
								"
							/>
							<install-configuration
								:name="t('detail.require_gpu')"
								:data="
									item && item.requiredGpu
										? getValueByUnit(
												item.requiredGpu,
												getSuitableUnit(item.requiredGpu, 'memory')
											)
										: '-'
								"
								:unit="
									item && item.requiredGpu
										? getSuitableUnit(item.requiredGpu, 'memory')
										: '-'
								"
								:last="true"
							/>
						</div>
					</div>

					<div
						class="full-width column"
						:class="
							$q.dark.isActive
								? 'app-detail-bottom-dark'
								: 'app-detail-bottom-light'
						"
						style="padding-bottom: 56px"
					>
						<app-store-swiper
							v-if="item && item.promoteImage"
							:data-array="item && item.promoteImage ? item.promoteImage : []"
							show-size="3,3,2"
							style="margin-bottom: 20px"
						>
							<template v-slot:swiper="{ item, index }">
								<q-img
									@click="showImage(index)"
									class="promote-img"
									ratio="1.6"
									:src="item"
								>
									<template v-slot:loading>
										<q-skeleton class="promote-img" style="height: 100%" />
									</template>
								</q-img>
							</template>
						</app-store-swiper>

						<div
							v-if="item"
							class="app-details-padding bottom-layout row justify-between"
						>
							<div class="column" style="width: calc(70% - 10px)">
								<app-intro-card
									:title="
										t('detail.about_this_type', {
											type: cfgType?.toUpperCase()
										})
									"
									:description="item.fullDescription"
								/>

								<app-intro-card
									class="q-mt-lg"
									:title="t('detail.whats_new')"
									:description="item.upgradeDescription"
								/>

								<app-intro-card
									v-if="requiredPermissions(cfgType)"
									class="q-mt-lg"
									:title="t('detail.required_permissions')"
								>
									<template v-slot:content>
										<div class="permission-request-grid column justify-start">
											<permission-request
												v-if="item.permission"
												name="sym_r_article"
												:title="t('permission.files')"
												:nodes="filePermissionData"
											/>
											<permission-request
												name="sym_r_language"
												:title="t('permission.internet')"
												:nodes="[
													{
														label: t('permission.internet_label'),
														children: [{ label: t('permission.internet_desc') }]
													}
												]"
											/>
											<permission-request
												name="sym_r_read_more"
												:title="t('permission.entrance')"
												:nodes="entrancePermissionData"
											/>
											<permission-request
												v-if="notificationPermission"
												name="sym_r_notifications"
												:title="t('permission.notifications')"
												:nodes="[
													{
														label: t('permission.notifications_label'),
														children: [
															{ label: t('permission.notifications_desc') }
														]
													}
												]"
											/>

											<permission-request
												v-if="
													item &&
													item.options &&
													item.options.analytics &&
													item.options.analytics.enable
												"
												name="sym_r_bar_chart_4_bars"
												:title="t('permission.analytics')"
												:nodes="[
													{
														label: t('permission.analytics_label'),
														children: [
															{ label: t('permission.analytics_desc') }
														]
													}
												]"
											/>

											<permission-request
												v-if="item && item.options && item.options.websocket"
												name="sym_r_captive_portal"
												:title="t('permission.websocket')"
												:nodes="[
													{
														label: t('permission.websocket_label'),
														children: [
															{ label: t('permission.websocket_desc') }
														]
													}
												]"
											/>

											<permission-request
												v-if="secretPermission"
												class="q-mt-lg"
												name="sym_r_lock_open"
												:title="t('permission.secret')"
												:nodes="[
													{
														label: t('permission.secret_label'),
														children: [{ label: t('permission.secret_desc') }]
													}
												]"
											/>

											<permission-request
												v-if="knowledgeBasePermission"
												name="sym_r_cognition"
												:title="t('permission.knowledgebase')"
												:nodes="[
													{
														label: t('permission.knowledgebase_label'),
														children: [
															{ label: t('permission.knowledgebase_desc') }
														]
													}
												]"
											/>

											<permission-request
												v-if="
													item && item.middleware && item.middleware.ZincSearch
												"
												class="q-mt-lg"
												name="sym_r_search"
												:title="t('base.search')"
												:nodes="[
													{
														label: t('permission.search_label')
													}
												]"
											/>

											<permission-request
												v-if="
													item && item.middleware && item.middleware.Postgres
												"
												name="sym_r_animation"
												:title="t('permission.relational_database')"
												:nodes="[
													{
														label: t('permission.relational_database_label'),
														children: [
															{
																label: t('permission.relational_database_desc')
															}
														]
													}
												]"
											/>

											<permission-request
												v-if="
													item && item.middleware && item.middleware.MongoDB
												"
												name="sym_r_edit_document"
												:title="t('permission.document_database')"
												:nodes="[
													{
														label: t('permission.document_database_label'),
														children: [
															{ label: t('permission.document_database_desc') }
														]
													}
												]"
											/>

											<permission-request
												v-if="item && item.middleware && item.middleware.Redis"
												name="sym_r_commit"
												:title="t('permission.key_value_database')"
												:nodes="[
													{
														label: t('permission.key_value_database_label'),
														children: [
															{ label: t('permission.key_value_database_desc') }
														]
													}
												]"
											/>

											<permission-request
												v-if="
													item &&
													item.options &&
													item.options.appScope &&
													item.options.appScope.clusterScoped
												"
												name="sym_r_lan"
												:title="t('base.cluster_app')"
												:nodes="[
													{
														label: t('permission.cluster_app_label')
													}
												]"
											/>
										</div>
									</template>
								</app-intro-card>

								<app-intro-card
									v-if="readMeHtml && item"
									class="q-mt-lg"
									style="width: 100%"
									:title="t('detail.readme')"
								>
									<template v-slot:content>
										<div
											style="width: 100%; max-width: 1086px"
											id="readMe"
											v-html="readMeHtml"
										/>
									</template>
								</app-intro-card>
							</div>

							<div
								class="column justify-start items-start"
								style="width: calc(30% - 10px)"
							>
								<app-intro-card :title="t('detail.information')">
									<template v-slot:content>
										<app-intro-item
											:title="t('detail.get_support')"
											:link-array="getDocuments()"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.website')"
											:link-array="getWebsite()"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.app_version')"
											:content="item.versionName"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('base.category')"
											separator=" · "
											:content-array="item.categories"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('base.developer')"
											:content="item.developer"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('base.submitter')"
											:content="item.submitter"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('base.language')"
											:content-array="
												item.language
													? convertLanguageCodesToNames(item.language)
													: ''
											"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.compatibility')"
											:content="compatible"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.platforms')"
											:content-array="item.supportArch"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.legal')"
											:link-array="
												item.legal && item.legal.length && item.legal[0]
													? [item.legal[0]]
													: []
											"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.license')"
											:link-array="
												item.license &&
												item.license.length > 0 &&
												item.license[0]
													? [item.license[0]]
													: []
											"
										/>
										<app-intro-item
											v-if="item.sourceCode"
											class="q-mt-lg"
											:title="t('detail.source_code')"
											:link-array="[
												{
													text: t('detail.public'),
													url: item.sourceCode
												}
											]"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.chart_version')"
											:content="item.version"
										/>
										<app-intro-item
											class="q-mt-lg"
											:title="t('detail.version_history')"
											@on-link-click="goVersionHistory"
											link="See all version"
										/>
									</template>
								</app-intro-card>

								<app-intro-card
									:title="t('detail.get_a_client')"
									class="q-mt-lg"
								>
									<template
										v-slot:content
										v-if="
											item.supportClient.ios ||
											item.supportClient.android ||
											item.supportClient.edge ||
											item.supportClient.windows ||
											item.supportClient.mac ||
											item.supportClient.chrome ||
											item.supportClient.linux
										"
									>
										<div class="row">
											<support-client
												:value="item.supportClient.android"
												:type="CLIENT_TYPE.android"
											/>
											<support-client
												:value="item.supportClient.ios"
												:type="CLIENT_TYPE.ios"
												class="q-mt-lg"
											/>
											<support-client
												:value="item.supportClient.mac"
												:type="CLIENT_TYPE.mac"
												class="q-mt-lg"
											/>
											<support-client
												:value="item.supportClient.windows"
												:type="CLIENT_TYPE.windows"
												class="q-mt-lg"
											/>
											<support-client
												:value="item.supportClient.chrome"
												:type="CLIENT_TYPE.chrome"
												class="q-mt-lg"
											/>
											<support-client
												:value="item.supportClient.edge"
												:type="CLIENT_TYPE.edge"
												class="q-mt-lg"
											/>
											<support-client
												:value="item.supportClient.linux"
												:type="CLIENT_TYPE.linux"
												class="q-mt-lg"
											/>
										</div>
									</template>
								</app-intro-card>

								<app-intro-card
									v-if="dependencies.length > 0"
									class="q-mt-lg"
									:title="t('detail.dependency')"
								>
									<template v-slot:content>
										<template :key="app.name" v-for="app in dependencies">
											<app-small-card :item="app" :is-last-line="true" />
										</template>
									</template>
								</app-intro-card>

								<app-intro-card
									v-if="references.length > 0"
									class="q-mt-lg"
									:title="t('detail.reference_app')"
								>
									<template v-slot:content>
										<template :key="app.name" v-for="app in references">
											<app-small-card :item="app" :is-last-line="true" />
										</template>
									</template>
								</app-intro-card>
							</div>
						</div>
					</div>
				</div>
			</template>
		</page-container>
	</div>
</template>

<script lang="ts" setup>
import { computed, onActivated, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAppStore } from 'src/stores/app';
import {
	APP_STATUS,
	AppStoreInfo,
	CLIENT_TYPE,
	DEPENDENCIES_TYPE,
	PERMISSION_SYSDATA_GROUP,
	PermissionNode,
	TRANSACTION_PAGE
} from 'src/constants/constants';
import InstallConfiguration from 'src/components/appintro/InstallConfiguration.vue';
import SupportClient from 'src/components/appintro/SupportClient.vue';
import { getApp, getMarkdown } from 'src/api/storeApi';
import PageContainer from 'src/components/base/PageContainer.vue';
import { bus, BUS_EVENT, updateAppStoreList } from 'src/utils/bus';
import AppIntroCard from 'src/components/appintro/AppIntroCard.vue';
import AppCard from 'src/components/appcard/AppCard.vue';
import AppIntroItem from 'src/components/appintro/AppIntroItem.vue';
import PermissionRequest from 'src/components/appintro/PermissionRequest.vue';
import AppStoreSwiper from 'src/components/base/AppStoreSwiper.vue';
import AppTitleBar from 'src/components/appintro/AppTitleBar.vue';
import AppSmallCard from 'src/components/appcard/AppSmallCard.vue';
import markdownit from 'markdown-it';
import hljs from 'highlight.js';
import ins_plugin from 'markdown-it-mark';
import { OnUpdateUITask, TaskHandler } from 'src/pages/application/AppDeatil';
import { getSuitableUnit, getValueByUnit } from 'src/utils/monitoring';
import {
	convertLanguageCodeToName,
	convertLanguageCodesToNames,
	capitalizeFirstLetter
} from 'src/utils/utils';
import { useI18n } from 'vue-i18n';
import TitleBar from 'src/components/base/TitleBar.vue';
import { copyToClipboard, useQuasar } from 'quasar';
import { BtNotify, NotifyDefinedType } from '@bytetrade/ui';
import { requiredPermissions, showIcon } from 'src/constants/config';

const route = useRoute();
const router = useRouter();
const appStore = useAppStore();
const { t } = useI18n();
const $q = useQuasar();
const item = ref<AppStoreInfo>();

const loadHighlightStyles = () => {
	const isDark = $q.dark.isActive;
	const path = isDark ? '../../css/github-dark.css' : '../../css/github.css';
	import(path)
		.then(() => {
			//Do nothing
		})
		.catch((err) => {
			console.error('Failed to load highlight.js style:', err);
		});
};
loadHighlightStyles();
const md = markdownit({
	html: true,
	linkify: true,
	typographer: true,
	highlight: function (str, lang) {
		// console.log('str ===> ', str);
		// console.log('lang ===> ', lang);
		if (lang && hljs.getLanguage(lang)) {
			try {
				return (
					// '<div class="code-container">' +
					// '<button class="copy-button text-caption q-ml-lg text-grey-8" @click="copyCode(\'' +
					// str +
					// '\')">Copy</button>' +
					'<pre><code class="hljs">' +
					hljs.highlight(str, { language: lang, ignoreIllegals: true }).value +
					'</code></pre>'
					// '</div>'
				);
			} catch (e) {
				console.log('error ===> ', e);
			}
		}

		return (
			'<pre><code class="hljs">' + md.utils.escapeHtml(str) + '</code></pre>'
		);
	}
}).use(ins_plugin);

const showHeaderBar = ref(false);
const dependencies = ref<AppStoreInfo[]>([]);
const references = ref<AppStoreInfo[]>([]);
const compatible = ref<string>('');
const entrancePermissionData = ref<PermissionNode[]>([]);
const filePermissionData = ref<PermissionNode[]>([]);
const language = ref('en');
const languageLength = ref(0);
const cfgType = ref<string>();
const readMeHtml = ref('');

function copyCode(str: string) {
	copyToClipboard(str)
		.then(() => {
			BtNotify.show({
				type: NotifyDefinedType.SUCCESS,
				message: t('base.copy_success')
			});
		})
		.catch(() => {
			BtNotify.show({
				type: NotifyDefinedType.FAILED,
				message: t('base.copy_failure')
			});
		});
}

const updateApp = (app: AppStoreInfo) => {
	console.log(`get status ${app.name}`);
	if (app && item.value && item.value.name === app.name) {
		item.value.status = app.status;
		item.value.uid = app.uid;
		console.log(`update status ${app.name}`);
		updateAppStoreList([item.value], app);
		updateAppStoreList(dependencies.value, app);
		updateAppStoreList(references.value, app);
	}
};

onMounted(() => {
	bus.on(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

onBeforeUnmount(() => {
	bus.off(BUS_EVENT.UPDATE_APP_STORE_INFO, updateApp);
});

onActivated(async () => {
	console.log('onActivated');
	cfgType.value = route.params.type as string;
	await setAppItem(route.params.name as string);
});

const getDocuments = () => {
	const data: { text: string; url: string }[] = [];
	if (!item.value) {
		return data;
	}
	if (item.value.doc) {
		data.push({ text: t('base.documents'), url: item.value.doc });
	}
	return data;
};

const getWebsite = () => {
	const data: { text: string; url: string }[] = [];
	if (!item.value) {
		return data;
	}
	if (item.value.website) {
		let show = item.value.website;
		try {
			const url = new URL(item.value?.website);
			show = url.hostname;
		} catch (e) {
			console.log(e);
		}

		data.push({ text: show, url: item.value.website });
	}
	return data;
};

const goVersionHistory = () => {
	const appName = route.params.name as string;
	if (appName) {
		router.push({
			name: TRANSACTION_PAGE.Version,
			params: {
				name: appName
			}
		});
	}
};

const setAppItem = async (name: string) => {
	if (!name) {
		console.log('app name empty');
		return;
	}

	let app = appStore.getAppItem(name);
	if (app) {
		item.value = app;
		console.log('load local app');
	} else {
		const response = await getApp(name);
		if (!response) {
			console.log('get app info failure');
			return;
		}
		appStore.tempAppMap[name] = response;
		item.value = response;
	}

	console.log(item.value);

	const taskHandler = new TaskHandler();
	taskHandler
		.addTask(dependenciesTask)
		.addTask(filePermissionTask)
		.addTask(entrancePermissionTask)
		.addTask(languageTask)
		.addTask(appScopeTask)
		.addTask(markdownTask)
		.doRunning(item.value);
};

const dependenciesTask: OnUpdateUITask = {
	onInit() {
		dependencies.value = [];
		compatible.value = '';
	},
	onUpdate(app: AppStoreInfo) {
		if (
			app?.options &&
			app?.options.dependencies &&
			app?.options.dependencies.length > 0
		) {
			app?.options.dependencies.forEach((appInfo) => {
				if (
					appInfo.type === DEPENDENCIES_TYPE.application ||
					appInfo.type === DEPENDENCIES_TYPE.middleware
				) {
					getApp(appInfo.name).then((app) => {
						if (app) {
							dependencies.value.push(app);
						}
					});
				}
			});
			app?.options.dependencies.forEach((appInfo) => {
				if (appInfo.type === DEPENDENCIES_TYPE.system) {
					compatible.value = `${compatible.value} ${capitalizeFirstLetter(appInfo.name)} ${appInfo.version}`;
				}
			});
		}
	}
};

const filePermissionTask: OnUpdateUITask = {
	onInit() {
		filePermissionData.value = [];
	},
	onUpdate(app: AppStoreInfo) {
		if (
			app.permission &&
			!app.permission.appData &&
			!app.permission.appCache &&
			(!app.permission.userData || app.permission.userData.length === 0)
		) {
			filePermissionData.value = [
				{ label: t('permission.files_not_store_label'), children: [] }
			];
			return;
		}

		if (
			app.permission &&
			app.permission.userData &&
			app.permission.userData.length > 0
		) {
			const nodes: PermissionNode[] = [];
			nodes.push({
				label: t('permission.files_access_user_data_label'),
				children: []
			});
			app.permission.userData.forEach((item) => {
				nodes.push({ label: '- ' + item, children: [] });
			});
			filePermissionData.value.push({
				label: t('permission.files_access_user_data_label'),
				children: nodes
			});
		}

		if (app.permission && app.permission.appData) {
			filePermissionData.value.push({
				label: t('permission.file_store_data_folder_label'),
				children: [
					{
						label: t('permission.file_store_data_folder_desc'),
						children: []
					}
				]
			});
		}

		if (app.permission && app.permission.appCache) {
			filePermissionData.value.push({
				label: t('permission.file_store_cache_folder_label'),
				children: [
					{
						label: t('permission.file_store_cache_folder_desc'),
						children: []
					}
				]
			});
		}
	}
};

const entrancePermissionTask: OnUpdateUITask = {
	onInit() {
		entrancePermissionData.value = [];
	},
	onUpdate(app: AppStoreInfo) {
		let desktopSize = 0;
		let backendSize = 0;
		let privateSize = 0;
		let publicSize = 0;
		let twoFactorTitle: string[] = [];
		if (app?.entrances && app?.entrances.length > 0) {
			app?.entrances.forEach((item) => {
				if (item.invisible) {
					backendSize++;
				} else {
					desktopSize++;
				}
				if (item.authLevel === 'private') {
					privateSize++;
				} else {
					publicSize++;
				}
			});
			if (
				app?.options &&
				app?.options.policies &&
				app?.options.policies.length > 0
			) {
				let twoFactorPolicies = app?.options.policies.filter((item) => {
					return item.level && item.level === 'two_factor';
				});

				if (twoFactorPolicies) {
					app?.entrances.forEach((item) => {
						twoFactorPolicies.forEach((policy) => {
							if (item.name === policy.entranceName) {
								twoFactorTitle.push(item.title);
							}
						});
					});
				}
			}
		}
		const firstLabel = t('permission.entrance_visibility_label', {
			desktopSize: `${desktopSize > 0 ? desktopSize : 'no'}`,
			backendSize: ` ${backendSize > 0 ? backendSize : 'no'}`
		});

		const secondLabel = t('permission.entrance_auth_level_label', {
			privateSize: `${privateSize > 0 ? privateSize : 'no'}`,
			publicSize: ` ${publicSize > 0 ? publicSize : 'no'}`
		});

		entrancePermissionData.value.push({
			label: firstLabel,
			children: [
				{ label: t('permission.entrance_visibility_desc_first'), children: [] },
				{ label: t('permission.entrance_visibility_desc_second'), children: [] }
			]
		});
		entrancePermissionData.value.push({
			label: secondLabel,
			children: [
				{ label: t('permission.entrance_auth_level_desc'), children: [] }
			]
		});
		if (twoFactorTitle.length > 0) {
			const label = t('permission.entrance_two_factor_label', {
				twoFactor: `${twoFactorTitle.join(',')}`
			});
			entrancePermissionData.value.push({ label, children: [] });
		}
	}
};

const appScopeTask: OnUpdateUITask = {
	onInit() {
		references.value = [];
	},
	onUpdate(app: AppStoreInfo) {
		if (
			app?.options &&
			app?.options.appScope &&
			app?.options.appScope.appRef &&
			app?.options.appScope.appRef.length > 0
		) {
			app?.options.appScope.appRef.forEach((appName) => {
				getApp(appName).then((app) => {
					if (app) {
						references.value.push(app);
					}
				});
			});
		}
	}
};

const languageTask: OnUpdateUITask = {
	onInit() {
		language.value = 'en';
		languageLength.value = 0;
	},
	onUpdate(app: AppStoreInfo) {
		if (app.language) {
			language.value = app.language[0];
			languageLength.value =
				app.language.length > 0 ? 0 : app.language.length - 1;
		}
	}
};

const markdownTask: OnUpdateUITask = {
	onInit() {
		readMeHtml.value = '';
	},
	onUpdate(app: AppStoreInfo) {
		getMarkdown(app.name).then((result) => {
			if (result) {
				console.log(result);
				readMeHtml.value = md.render(result);
				// console.log(readMeHtml.value);
			}
		});
	}
};

const notificationPermission = computed(() => {
	const app = item.value;
	if (
		app &&
		app.permission &&
		app.permission.sysData &&
		app.permission.sysData.length > 0
	) {
		for (let i = 0; i < app.permission.sysData.length; i++) {
			const sysdata = app.permission.sysData[i];
			if (sysdata.group === PERMISSION_SYSDATA_GROUP.service_notification) {
				return true;
			}
		}
	}
	return false;
});

const secretPermission = computed(() => {
	const app = item.value;
	if (
		app &&
		app.permission &&
		app.permission.sysData &&
		app.permission.sysData.length > 0
	) {
		for (let i = 0; i < app.permission.sysData.length; i++) {
			const sysdata = app.permission.sysData[i];
			if (
				sysdata.group === PERMISSION_SYSDATA_GROUP.secret_infisical ||
				sysdata.group === PERMISSION_SYSDATA_GROUP.secret_vault
			) {
				return true;
			}
		}
	}
	return false;
});

const knowledgeBasePermission = computed(() => {
	const app = item.value;
	if (
		app &&
		app.permission &&
		app.permission.sysData &&
		app.permission.sysData.length > 0
	) {
		for (let i = 0; i < app.permission.sysData.length; i++) {
			const sysdata = app.permission.sysData[i];
			if (sysdata.group === PERMISSION_SYSDATA_GROUP.service_search) {
				return true;
			}
		}
	}
	return false;
});

const showImage = (index: number) => {
	router.push({
		name: TRANSACTION_PAGE.Preview,
		params: {
			name: route.params.name as string,
			index: index
		}
	});
};

const clickReturn = () => {
	appStore.removeAppItem(route.params.name as string);
	router.back();
};
</script>
<style lang="scss" scoped>
.app-detail-root {
	width: 100%;
	height: 100%;

	.app-detail-page {
		width: 100%;
		height: 100%;
		position: relative;

		.app-details-padding {
			padding-left: 44px;
			padding-right: 44px;
		}

		.app-detail-warning {
			margin-left: 44px;
			width: calc(100% - 88px);
			padding: 12px;
			margin-bottom: 20px;
			border-radius: 12px;
		}

		.promote-img {
			border-radius: 20px;
			width: 100%;
		}

		.app-detail-line {
			margin-left: 44px;
			width: calc(100% - 88px);
			height: 1px;
			background: $separator;
		}

		.permission-request-grid {
			width: 100%;
			display: grid;
			align-items: start;
			justify-items: center;
			justify-content: center;
			grid-row-gap: 20px;

			@media (max-width: 1440px) {
				grid-template-columns: repeat(1, minmax(0, 1fr));
			}

			@media (min-width: 1441px) {
				grid-template-columns: repeat(2, minmax(0, 1fr));
			}
		}

		.app-detail-install-configuration {
			width: 100%;
			height: 112px;
			display: grid;
			align-items: start;
			justify-items: start;
			justify-content: start;
			grid-template-columns: repeat(var(--showSize), minmax(80px, 1fr));
			padding-bottom: 20px;
			padding-top: 20px;
		}

		.bottom-layout {
			height: 100%;
			width: 100%;
		}
	}
}

.app-detail-top-dark {
	background: linear-gradient(175.85deg, #1f1f1f 3.38%, #12191d 96.62%);
}

.app-detail-bottom-dark {
	background: rgba(18, 25, 29, 1);
}

.app-detail-top-light {
	background: linear-gradient(175.85deg, #ffffff 3.38%, #f1f9ff 96.62%);
}

.app-detail-bottom-light {
	background: rgba(241, 249, 255, 1);
}
</style>
