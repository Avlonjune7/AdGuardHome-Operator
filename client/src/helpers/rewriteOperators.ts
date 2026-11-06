export const rewriteOperators = [
    { value: '', label: '默认' },
    { value: 'cmcc', label: '中国移动' },
    { value: 'unicom', label: '中国联通' },
    { value: 'chinanet', label: '中国电信' },
] as const;

export const operatorLabel = (operator?: string): string =>
    rewriteOperators.find(({ value }) => value === operator)?.label ?? rewriteOperators[0].label;
