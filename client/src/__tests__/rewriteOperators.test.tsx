import React from 'react';
import ReactDOM from 'react-dom';
import { act } from 'react-dom/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Form from '../components/Filters/Rewrites/Form';
import i18n from '../i18n';
import apiClient from '../api/Api';
import { operatorLabel } from '../helpers/rewriteOperators';

let container: HTMLDivElement;
afterEach(() => {
    if (container) {
        ReactDOM.unmountComponentAtNode(container);
        container.remove();
    }
    vi.restoreAllMocks();
});

describe('operator rewrites', () => {
    it('submits the selected operator and preserves a disabled rule when editing', async () => {
        container = document.createElement('div');
        document.body.appendChild(container);
        const submit = vi.fn();
        act(() => {
            ReactDOM.render(<Form processingAdd={false} toggleRewritesModal={() => {}}
                currentRewrite={{ domain: 'example.org', answer: '192.0.2.1', operator: 'cmcc', enabled: false }}
                onSubmit={submit} />, container);
        });
        const select = container.querySelector('select')!;
        expect(select.value).toBe('cmcc');
        expect(Array.from(select.options).map((option) => option.text)).toEqual(['默认', '中国移动', '中国联通', '中国电信']);
        await act(async () => {
            select.value = 'unicom';
            select.dispatchEvent(new Event('change', { bubbles: true }));
        });
        await act(async () => {
            container.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
        });
        expect(submit).toHaveBeenCalledWith({ domain: 'example.org', answer: '192.0.2.1', operator: 'unicom', enabled: false });
    });

    it('uses Chinese labels and only bundles the Chinese locale', () => {
        expect(operatorLabel()).toBe('默认');
        expect(operatorLabel('chinanet')).toBe('中国电信');
        expect(i18n.language).toBe('zh-cn');
        expect(Object.keys(i18n.options.resources!)).toEqual(['zh-cn']);
        expect(i18n.t('dns_rewrites')).toBe('DNS 重写');
    });

    it('sends the operator identity when deleting a rule', async () => {
        const request = vi.spyOn(apiClient, 'makeRequest').mockResolvedValue({});
        const rule = { domain: 'example.org', answer: '192.0.2.1', operator: 'cmcc' };
        await apiClient.deleteRewrite(rule);
        expect(request).toHaveBeenCalledWith('rewrite/delete', 'POST', { data: rule });
    });
});
