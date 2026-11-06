import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { setHtmlLangAttr } from './helpers/helpers';
import zhCN from './__locales/zh-cn.json';
import zhCNServices from './__locales-services/zh-cn.json';

i18n.use(initReactI18next).init({
    resources: {
        'zh-cn': {
            translation: zhCN,
            services: Object.fromEntries(Object.entries(zhCNServices).map(([key, value]) => [key, value.message])),
        },
    },
    lng: 'zh-cn',
    fallbackLng: 'zh-cn',
    whitelist: ['zh-cn'],
    load: 'currentOnly',
    lowerCaseLng: true,
    keySeparator: false,
    nsSeparator: false,
    returnEmptyString: false,
    ns: ['translation', 'services'],
    defaultNS: 'translation',
    interpolation: { escapeValue: false },
}, () => setHtmlLangAttr('zh-cn'));

export default i18n;
