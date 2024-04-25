import { MENU_TYPE, TRANSACTION_PAGE } from '../constants/constants.ts';
import MainLayout from 'layouts/MainLayout.vue';

const routes = [
	{
		path: '/',
		component: MainLayout,
		children: [
			{
				path: '/',
				name: MENU_TYPE.Application.Home,
				component: () => import('pages/application/HomePage.vue')
			},
			{
				path: 'category/:categories',
				name: 'Category',
				component: () => import('pages/application/CategoryPage.vue')
			},
			{
				path: '/:type/:name',
				name: TRANSACTION_PAGE.App,
				component: () => import('pages/application/AppDetailPage.vue')
			},
			{
				path: 'list/:categories/:type?',
				name: TRANSACTION_PAGE.List,
				component: () => import('pages/application/AppListPage.vue')
			},
			{
				path: 'discover/:categories/:topicId',
				name: TRANSACTION_PAGE.Discover,
				component: () => import('pages/application/TopicPage.vue')
			},
			{
				path: '/preview/:name/:index',
				name: TRANSACTION_PAGE.Preview,
				component: () => import('pages/application/AppImagePreviewPage.vue')
			},
			{
				path: 'recommend',
				name: 'Recommend',
				component: () => import('pages/recommend/RecommendPage.vue')
			},
			{
				path: 'model',
				name: 'Model',
				component: () => import('pages/agent/AgentPage.vue')
			},
			{
				path: '/terminus',
				name: MENU_TYPE.MyTerminus,
				component: () => import('pages/me/MyTerminusPage.vue')
			},
			{
				path: '/log/:type',
				name: TRANSACTION_PAGE.Log,
				component: () => import('pages/me/LogPage.vue')
			},
			{
				path: '/update',
				name: TRANSACTION_PAGE.Update,
				component: () => import('pages/me/UpdatePage.vue')
			},
			{
				path: '/versionHistory/:name',
				name: TRANSACTION_PAGE.Version,
				component: () => import('pages/application/VersionHistoryPage.vue')
			}
		]
	},

	// Always leave this as last one,
	// but you can also remove it
	{
		path: '/:catchAll(.*)*',
		component: () => import('pages/ErrorNotFound.vue')
	}
];

export default routes;
