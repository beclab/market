export const utcToStamp = (utc_datetime: string) => {
	const T_pos = utc_datetime.indexOf('T');
	const Z_pos = utc_datetime.indexOf('Z');
	const year_month_day = utc_datetime.substr(0, T_pos);
	const hour_minute_second = utc_datetime.substr(T_pos + 1, Z_pos - T_pos - 1);
	const new_datetime = year_month_day + ' ' + hour_minute_second;
	return new Date(Date.parse(new_datetime));
};

export const getSliceArray = (list: any, size: number) => {
	if (list.length > size) {
		return list.slice(0, size);
	} else {
		return list;
	}
};

export function humanStorageSize(bytes: any) {
	const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
	let u = 0;

	while (parseInt(bytes, 10) >= 1024 && u < units.length - 1) {
		bytes /= 1024;
		++u;
	}

	return { value: bytes.toFixed(1), unit: units[u] };
}

export function formatTimeDifference(utcTimeString: string): string {
	const utcDate = new Date(utcTimeString);
	const currentDate = new Date();

	const timeDifference = currentDate.getTime() - utcDate.getTime();
	const secondsDifference = Math.floor(timeDifference / 1000);
	const minutesDifference = Math.floor(secondsDifference / 60);
	const hoursDifference = Math.floor(minutesDifference / 60);
	const daysDifference = Math.floor(hoursDifference / 24);
	const weeksDifference = Math.floor(daysDifference / 7);
	const monthsDifference = Math.floor(daysDifference / 30);
	const yearsDifference = Math.floor(daysDifference / 365);

	if (daysDifference === 0) {
		return 'Today';
	} else if (daysDifference === 1) {
		return 'Yesterday';
	} else if (daysDifference < 7) {
		return `${daysDifference} days ago`;
	} else if (weeksDifference === 1) {
		return '1 week ago';
	} else if (weeksDifference < 4) {
		return `${weeksDifference} weeks ago`;
	} else if (monthsDifference === 1) {
		return '1 month ago';
	} else if (monthsDifference < 12) {
		return `${monthsDifference} months ago`;
	} else if (yearsDifference === 1) {
		return '1 year ago';
	} else if (yearsDifference <= 2) {
		return '2 years ago';
	} else {
		return utcDate.toLocaleDateString('en-US', {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}
}

export function capitalizeFirstLetter(str: string): string {
	if (str.length > 0) {
		return str.charAt(0).toUpperCase() + str.slice(1);
	}
	return str;
}

export function decodeUnicode(str) {
	return str.replace(/\\u[\dA-F]{4}/gi, function (match) {
		return String.fromCharCode(parseInt(match.replace(/\\u/g, ''), 16));
	});
}

export function intersection<T>(array1: T[], array2: T[]): T[] {
	return array1.filter((value) => array2.includes(value));
}

export class Range {
	operators = ['>=', '>', '<=', '<', '='];
	operatorIndex = -1;
	rangeVersion: Version | null = null;

	constructor(range: string) {
		this.operatorIndex = this.operators.findIndex((op) => range.startsWith(op));
		if (this.operatorIndex === -1) {
			throw new Error(`Invalid range: ${range}`);
		}

		const version = range.slice(this.operators[this.operatorIndex].length);
		this.rangeVersion = new Version(version);
	}

	satisfies(check: Version): boolean {
		if (this.rangeVersion) {
			switch (this.operators[this.operatorIndex]) {
				case '>=':
					return (
						check.major > this.rangeVersion.major ||
						(check.major === this.rangeVersion.major &&
							(check.minor > this.rangeVersion.minor ||
								(check.minor === this.rangeVersion.minor &&
									(check.patch > this.rangeVersion.patch ||
										(check.patch === this.rangeVersion.patch &&
											check.preRelease >= this.rangeVersion.preRelease)))))
					);
				case '>':
					return (
						check.major > this.rangeVersion.major ||
						(check.major === this.rangeVersion.major &&
							(check.minor > this.rangeVersion.minor ||
								(check.minor === this.rangeVersion.minor &&
									(check.patch > this.rangeVersion.patch ||
										(check.patch === this.rangeVersion.patch &&
											check.preRelease > this.rangeVersion.preRelease)))))
					);
				case '<=':
					return (
						check.major < this.rangeVersion.major ||
						(check.major === this.rangeVersion.major &&
							(check.minor < this.rangeVersion.minor ||
								(check.minor === this.rangeVersion.minor &&
									(check.patch < this.rangeVersion.patch ||
										(check.patch === this.rangeVersion.patch &&
											check.preRelease <= this.rangeVersion.preRelease)))))
					);
				case '<':
					return (
						check.major < this.rangeVersion.major ||
						(check.major === this.rangeVersion.major &&
							(check.minor < this.rangeVersion.minor ||
								(check.minor === this.rangeVersion.minor &&
									(check.patch < this.rangeVersion.patch ||
										(check.patch === this.rangeVersion.patch &&
											check.preRelease < this.rangeVersion.preRelease)))))
					);
				case '=':
					return (
						check.major === this.rangeVersion.major &&
						check.minor === this.rangeVersion.minor &&
						check.patch === this.rangeVersion.patch &&
						check.preRelease === this.rangeVersion.preRelease
					);
				default:
					throw new Error(
						`Invalid operator: ${this.operators[this.operatorIndex]}`
					);
			}
		} else {
			throw new Error(`Invalid range: ${this.rangeVersion}`);
		}
	}
}

export class Version {
	major = 0;
	minor = 0;
	patch = 0;
	preRelease = Number.MAX_VALUE;

	constructor(version: string) {
		if (!version.includes('.')) {
			throw new Error('error version');
		}
		let versionData;
		if (version.includes('-')) {
			const array = version.split('-');
			if (array.length > 0) {
				versionData = array[0];
				this.preRelease = Number(array[1]);
			} else {
				throw new Error('error version');
			}
		} else {
			versionData = version;
		}

		const versionArray = versionData.split('.');
		if (versionArray.length <= 2) {
			throw new Error('error version');
		}
		this.major = Number(versionArray[0]);
		this.minor = Number(versionArray[1]);
		this.patch = Number(versionArray[2]);
	}
}

export function testSatisfies() {
	const operator = '>=';
	console.log(
		new Range(operator + '0.3.0.1-22').satisfies(
			new Version('0.3.0.1-20140125')
		)
	);
	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.3.0.1-20140125'))
	);
	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.3.0.1-20140125'))
	);
	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(
			new Version('0.3.0.1-20140125')
		)
	);

	console.log(
		new Range(operator + '0.3.0.1-22').satisfies(
			new Version('0.4.1.5-20140125')
		)
	);
	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.4.1.5-20140125'))
	);
	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.4.1.5-20140125'))
	);
	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(
			new Version('0.4.1.5-20140125')
		)
	);

	console.log(
		new Range(operator + '0.3.0.1-22').satisfies(
			new Version('0.2.1.5-20140125')
		)
	);
	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.2.1.5-20140125'))
	);
	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.2.1.5-20140125'))
	);
	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(
			new Version('0.2.1.5-20140125')
		)
	);

	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.3.0.1-0'))
	);
	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.3.0.1-22'))
	);
	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.3.0.1'))
	);
	console.log(
		new Range(operator + '0.3.0.1').satisfies(new Version('0.3.0.1-20140125'))
	);

	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.3.0.1-0'))
	);
	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.3.0.1-22'))
	);
	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.3.0.1'))
	);
	console.log(
		new Range(operator + '0.3.0.1-0').satisfies(new Version('0.3.0.1-20140125'))
	);

	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(new Version('0.3.0.1-0'))
	);
	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(
			new Version('0.3.0.1-22')
		)
	);
	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(new Version('0.3.0.1'))
	);
	console.log(
		new Range(operator + '0.3.0.1-20140125').satisfies(
			new Version('0.3.0.1-20140125')
		)
	);
}

const languageMap: LanguageMap = {
	af: 'Afrikaans',
	'af-ZA': 'Afrikaans (South Africa)',
	ar: 'Arabic',
	'ar-AE': 'Arabic (U.A.E.)',
	'ar-BH': 'Arabic (Bahrain)',
	'ar-DZ': 'Arabic (Algeria)',
	'ar-EG': 'Arabic (Egypt)',
	'ar-IQ': 'Arabic (Iraq)',
	'ar-JO': 'Arabic (Jordan)',
	'ar-KW': 'Arabic (Kuwait)',
	'ar-LB': 'Arabic (Lebanon)',
	'ar-LY': 'Arabic (Libya)',
	'ar-MA': 'Arabic (Morocco)',
	'ar-OM': 'Arabic (Oman)',
	'ar-QA': 'Arabic (Qatar)',
	'ar-SA': 'Arabic (Saudi Arabia)',
	'ar-SY': 'Arabic (Syria)',
	'ar-TN': 'Arabic (Tunisia)',
	'ar-YE': 'Arabic (Yemen)',
	az: 'Azeri (Latin)',
	'az-AZ': 'Azeri (Latin) (Azerbaijan)',
	// 'az-AZ': 'Azeri (Cyrillic) (Azerbaijan)',
	be: 'Belarusian',
	'be-BY': 'Belarusian (Belarus)',
	bg: 'Bulgarian',
	'bg-BG': 'Bulgarian (Bulgaria)',
	'bs-BA': 'Bosnian (Bosnia and Herzegovina)',
	ca: 'Catalan',
	'ca-ES': 'Catalan (Spain)',
	cs: 'Czech',
	'cs-CZ': 'Czech (Czech Republic)',
	cy: 'Welsh',
	'cy-GB': 'Welsh (United Kingdom)',
	da: 'Danish',
	'da-DK': 'Danish (Denmark)',
	de: 'German',
	'de-AT': 'German (Austria)',
	'de-CH': 'German (Switzerland)',
	'de-DE': 'German (Germany)',
	'de-LI': 'German (Liechtenstein)',
	'de-LU': 'German (Luxembourg)',
	dv: 'Divehi',
	'dv-MV': 'Divehi (Maldives)',
	el: 'Greek',
	'el-GR': 'Greek (Greece)',
	en: 'English',
	'en-AU': 'English (Australia)',
	'en-BZ': 'English (Belize)',
	'en-CA': 'English (Canada)',
	'en-CB': 'English (Caribbean)',
	'en-GB': 'English (United Kingdom)',
	'en-IE': 'English (Ireland)',
	'en-JM': 'English (Jamaica)',
	'en-NZ': 'English (New Zealand)',
	'en-PH': 'English (Republic of the Philippines)',
	'en-TT': 'English (Trinidad and Tobago)',
	'en-US': 'English (United States)',
	'en-ZA': 'English (South Africa)',
	'en-ZW': 'English (Zimbabwe)',
	eo: 'Esperanto',
	es: 'Spanish',
	'es-AR': 'Spanish (Argentina)',
	'es-BO': 'Spanish (Bolivia)',
	'es-CL': 'Spanish (Chile)',
	'es-CO': 'Spanish (Colombia)',
	'es-CR': 'Spanish (Costa Rica)',
	'es-DO': 'Spanish (Dominican Republic)',
	'es-EC': 'Spanish (Ecuador)',
	'es-ES': 'Spanish (Castilian)',
	// 'es-ES': 'Spanish (Spain)',
	'es-GT': 'Spanish (Guatemala)',
	'es-HN': 'Spanish (Honduras)',
	'es-MX': 'Spanish (Mexico)',
	'es-NI': 'Spanish (Nicaragua)',
	'es-PA': 'Spanish (Panama)',
	'es-PE': 'Spanish (Peru)',
	'es-PR': 'Spanish (Puerto Rico)',
	'es-PY': 'Spanish (Paraguay)',
	'es-SV': 'Spanish (El Salvador)',
	'es-UY': 'Spanish (Uruguay)',
	'es-VE': 'Spanish (Venezuela)',
	et: 'Estonian',
	'et-EE': 'Estonian (Estonia)',
	eu: 'Basque',
	'eu-ES': 'Basque (Spain)',
	fa: 'Farsi',
	'fa-IR': 'Farsi (Iran)',
	fi: 'Finnish',
	'fi-FI': 'Finnish (Finland)',
	fo: 'Faroese',
	'fo-FO': 'Faroese (Faroe Islands)',
	fr: 'French',
	'fr-BE': 'French (Belgium)',
	'fr-CA': 'French (Canada)',
	'fr-CH': 'French (Switzerland)',
	'fr-FR': 'French (France)',
	'fr-LU': 'French (Luxembourg)',
	'fr-MC': 'French (Principality of Monaco)',
	gl: 'Galician',
	'gl-ES': 'Galician (Spain)',
	gu: 'Gujarati',
	'gu-IN': 'Gujarati (India)',
	he: 'Hebrew',
	'he-IL': 'Hebrew (Israel)',
	hi: 'Hindi',
	'hi-IN': 'Hindi (India)',
	hr: 'Croatian',
	'hr-BA': 'Croatian (Bosnia and Herzegovina)',
	'hr-HR': 'Croatian (Croatia)',
	hu: 'Hungarian',
	'hu-HU': 'Hungarian (Hungary)',
	hy: 'Armenian',
	'hy-AM': 'Armenian (Armenia)',
	id: 'Indonesian',
	'id-ID': 'Indonesian (Indonesia)',
	is: 'Icelandic',
	'is-IS': 'Icelandic (Iceland)',
	it: 'Italian',
	'it-CH': 'Italian (Switzerland)',
	'it-IT': 'Italian (Italy)',
	ja: 'Japanese',
	'ja-JP': 'Japanese (Japan)',
	ka: 'Georgian',
	'ka-GE': 'Georgian (Georgia)',
	kk: 'Kazakh',
	'kk-KZ': 'Kazakh (Kazakhstan)',
	kn: 'Kannada',
	'kn-IN': 'Kannada (India)',
	ko: 'Korean',
	'ko-KR': 'Korean (Korea)',
	kok: 'Konkani',
	'kok-IN': 'Konkani (India)',
	ky: 'Kyrgyz',
	'ky-KG': 'Kyrgyz (Kyrgyzstan)',
	lt: 'Lithuanian',
	'lt-LT': 'Lithuanian (Lithuania)',
	lv: 'Latvian',
	'lv-LV': 'Latvian (Latvia)',
	mi: 'Maori',
	'mi-NZ': 'Maori (New Zealand)',
	mk: 'FYRO Macedonian',
	'mk-MK': 'FYRO Macedonian (Former Yugoslav Republic of Macedonia)',
	mn: 'Mongolian',
	'mn-MN': 'Mongolian (Mongolia)',
	mr: 'Marathi',
	'mr-IN': 'Marathi (India)',
	ms: 'Malay',
	'ms-BN': 'Malay (Brunei Darussalam)',
	'ms-MY': 'Malay (Malaysia)',
	mt: 'Maltese',
	'mt-MT': 'Maltese (Malta)',
	nb: 'Norwegian (Bokmål)',
	'nb-NO': 'Norwegian (Bokmål) (Norway)',
	nl: 'Dutch',
	'nl-BE': 'Dutch (Belgium)',
	'nl-NL': 'Dutch (Netherlands)',
	'nn-NO': 'Norwegian (Nynorsk) (Norway)',
	ns: 'Northern Sotho',
	'ns-ZA': 'Northern Sotho (South Africa)',
	pa: 'Punjabi',
	'pa-IN': 'Punjabi (India)',
	pl: 'Polish',
	'pl-PL': 'Polish (Poland)',
	ps: 'Pashto',
	'ps-AR': 'Pashto (Afghanistan)',
	pt: 'Portuguese',
	'pt-BR': 'Portuguese (Brazil)',
	'pt-PT': 'Portuguese (Portugal)',
	qu: 'Quechua',
	'qu-BO': 'Quechua (Bolivia)',
	'qu-EC': 'Quechua (Ecuador)',
	'qu-PE': 'Quechua (Peru)',
	ro: 'Romanian',
	'ro-RO': 'Romanian (Romania)',
	ru: 'Russian',
	'ru-RU': 'Russian (Russia)',
	sa: 'Sanskrit',
	'sa-IN': 'Sanskrit (India)',
	se: 'Sami (Northern)',
	'se-FI': 'Sami (Northern) (Finland)',
	// 'se-FI': 'Sami (Skolt) (Finland)',
	// 'se-FI': 'Sami (Inari) (Finland)',
	'se-NO': 'Sami (Northern) (Norway)',
	// 'se-NO': 'Sami (Lule) (Norway)',
	// 'se-NO': 'Sami (Southern) (Norway)',
	'se-SE': 'Sami (Northern) (Sweden)',
	// 'se-SE': 'Sami (Lule) (Sweden)',
	// 'se-SE': 'Sami (Southern) (Sweden)',
	sk: 'Slovak',
	'sk-SK': 'Slovak (Slovakia)',
	sl: 'Slovenian',
	'sl-SI': 'Slovenian (Slovenia)',
	sq: 'Albanian',
	'sq-AL': 'Albanian (Albania)',
	'sr-BA': 'Serbian (Latin) (Bosnia and Herzegovina)',
	// 'sr-BA': 'Serbian (Cyrillic) (Bosnia and Herzegovina)',
	'sr-SP': 'Serbian (Latin) (Serbia and Montenegro)',
	// 'sr-SP': 'Serbian (Cyrillic) (Serbia and Montenegro)',
	sv: 'Swedish',
	'sv-FI': 'Swedish (Finland)',
	'sv-SE': 'Swedish (Sweden)',
	sw: 'Swahili',
	'sw-KE': 'Swahili (Kenya)',
	syr: 'Syriac',
	'syr-SY': 'Syriac (Syria)',
	ta: 'Tamil',
	'ta-IN': 'Tamil (India)',
	te: 'Telugu',
	'te-IN': 'Telugu (India)',
	th: 'Thai',
	'th-TH': 'Thai (Thailand)',
	tl: 'Tagalog',
	'tl-PH': 'Tagalog (Philippines)',
	tn: 'Tswana',
	'tn-ZA': 'Tswana (South Africa)',
	tr: 'Turkish',
	'tr-TR': 'Turkish (Turkey)',
	tt: 'Tatar',
	'tt-RU': 'Tatar (Russia)',
	ts: 'Tsonga',
	uk: 'Ukrainian',
	'uk-UA': 'Ukrainian (Ukraine)',
	ur: 'Urdu',
	'ur-PK': 'Urdu (Islamic Republic of Pakistan)',
	uz: 'Uzbek (Latin)',
	'uz-UZ': 'Uzbek (Latin) (Uzbekistan)',
	// 'uz-UZ': 'Uzbek (Cyrillic) (Uzbekistan)',
	vi: 'Vietnamese',
	'vi-VN': 'Vietnamese (Viet Nam)',
	xh: 'Xhosa',
	'xh-ZA': 'Xhosa (South Africa)',
	zh: 'Chinese',
	'zh-CN': 'Chinese (S)',
	'zh-HK': 'Chinese (Hong Kong)',
	'zh-MO': 'Chinese (Macau)',
	'zh-SG': 'Chinese (Singapore)',
	'zh-TW': 'Chinese (T)',
	zu: 'Zulu',
	'zu-ZA': 'Zulu (South Africa)'
};

interface LanguageMap {
	[key: string]: string;
}

export function convertLanguageCodeToName(code: string) {
	return '' || languageMap[code];
}

export function convertLanguageCodesToNames(codes: string[]): string[] {
	if (!codes || codes.length === 0) {
		return [];
	}

	return codes.map((code) => languageMap[code] || 'code');
}

export function showIconAddress(name: string): string {
	const src = '/appIcons/' + name + '.svg';
	return src;
}
