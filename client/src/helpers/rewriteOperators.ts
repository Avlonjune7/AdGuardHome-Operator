export const operatorLabel = (operator?: string): string => {
    switch (operator) {
        case 'cmcc': return '中国移动';
        case 'unicom': return '中国联通';
        case 'chinanet': return '中国电信';
        default: return '默认';
    }
};
