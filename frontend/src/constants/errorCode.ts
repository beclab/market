import { AppStoreInfo } from 'src/constants/constants';

interface SubErrorGroup {
	parentCode: string;
	code: string;
	title: string;
	variables?: Record<string, string | number>;
}

export interface ErrorGroup {
	code: ErrorCode;
	title: string;
	variables?: Record<string, string | number>;
	subGroups: SubErrorGroup[];
}

export enum ErrorCode {
	G001 = 'G001',
	G002 = 'G002',
	G003 = 'G003',
	G004 = 'G004',
	G005 = 'G005',
	G006 = 'G006',
	G007 = 'G007',
	G008 = 'G008',
	G009 = 'G009',
	G010 = 'G010',
	G011 = 'G011',
	G012 = 'G012',
	G013 = 'G013',
	G014 = 'G014',
	G015 = 'G015',
	G016 = 'G016',
	G017 = 'G017',
	G018 = 'G018',
	G019 = 'G019',
	G012_SG001 = 'G012-SG001',
	G012_SG002 = 'G012-SG002',
	G019_SG001 = 'G019-SG001'
}

const level1Map = new Map<ErrorCode, string>([
	[ErrorCode.G001, 'error.unknown_error'],
	[ErrorCode.G002, 'error.app_info_get_failure'],
	[ErrorCode.G003, 'error.failed_get_user_role'],
	[ErrorCode.G004, 'error.only_be_installed_by_the_admin'],
	[ErrorCode.G005, 'error.not_admin_role_install_middleware'],
	[ErrorCode.G006, 'error.not_admin_role_install_cluster_app'],
	[ErrorCode.G007, 'error.failed_to_get_os_version'],
	[ErrorCode.G008, 'error.app_is_not_compatible_terminus_os'],
	[ErrorCode.G009, 'error.failed_to_get_user_resource'],
	[ErrorCode.G010, 'error.user_not_enough_cpu'],
	[ErrorCode.G011, 'error.user_not_enough_memory'],
	[ErrorCode.G012, 'error.need_to_install_dependent_app_first'],
	[ErrorCode.G013, 'error.failed_to_get_system_resource'],
	[ErrorCode.G014, 'error.terminus_not_enough_cpu'],
	[ErrorCode.G015, 'error.terminus_not_enough_memory'],
	[ErrorCode.G016, 'error.terminus_not_enough_disk'],
	[ErrorCode.G017, 'error.terminus_not_enough_gpu'],
	[ErrorCode.G018, 'error.cluster_not_support_platform'],
	[ErrorCode.G019, 'error.app_install_conflict']
]);

const level2Map = new Map<ErrorCode, string>([
	[ErrorCode.G012_SG001, 'error.middleware_not_install_details'],
	[ErrorCode.G012_SG002, 'error.app_not_install_details'],
	[ErrorCode.G019_SG001, 'error.app_install_conflict_details']
]);

function findLevel1Error(
	code: string,
	variables?: Record<string, string | number>
): ErrorGroup | null {
	const level1Result = level1Map.get(code as ErrorCode);
	if (level1Result) {
		return {
			code: code as ErrorCode,
			title: level1Result,
			variables,
			subGroups: []
		};
	}
	return null;
}

function findLevel2Error(
	code: ErrorCode,
	variables?: Record<string, string | number>
): SubErrorGroup | null {
	const level2Result = level2Map.get(code);
	if (level2Result) {
		const [parentCode, subCode] = code.split('-');
		return { parentCode, code: subCode, title: level2Result, variables };
	}
	return null;
}

export function appPushError(
	app: AppStoreInfo,
	code: ErrorCode,
	variables?: Record<string, string | number>
) {
	if (!app || !code) return;

	if (code.indexOf('-') === -1) {
		const level1Error = findLevel1Error(code, variables);
		if (level1Error && !app.preflightError.find((e) => e.code === code)) {
			app.preflightError.push(level1Error);
		}
		return;
	}

	const level2Error = findLevel2Error(code, variables);
	if (level2Error) {
		const parentCode = level2Error.parentCode;
		let parentError: ErrorGroup | undefined | null = app.preflightError.find(
			(e) => e.code === parentCode
		);

		if (!parentError) {
			parentError = findLevel1Error(parentCode, variables);
			if (parentError) {
				app.preflightError.push(parentError);
			}
		}

		if (parentError) {
			parentError.subGroups.push(level2Error);
		}
	}
}

export function sortErrorGroups(errorGroups: ErrorGroup[]): ErrorGroup[] {
	errorGroups.sort((a, b) => a.code.localeCompare(b.code));

	errorGroups.forEach((group) => {
		group.subGroups.sort((a, b) => a.code.localeCompare(b.code));
	});

	return errorGroups;
}
