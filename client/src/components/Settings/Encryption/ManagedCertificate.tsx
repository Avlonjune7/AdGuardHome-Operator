import React, { useEffect, useRef, useState } from 'react';
import api from '../../../api/Api';

type Settings = { Enabled: boolean; Target: string; Email: string; Method?: string };
type Status = {
    settings: Settings;
    hasCFToken: boolean;
    running: boolean;
    supported: boolean;
    message: string;
    expires: string;
    cert: string;
    key: string;
};

export const ManagedCertificate = ({ usePaths }: { usePaths: (cert: string, key: string) => void }) => {
    const [status, setStatus] = useState<Status>();
    const [target, setTarget] = useState('');
    const [method, setMethod] = useState('http');
    const [token, setToken] = useState('');
    const [email, setEmail] = useState('');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const mounted = useRef(false);
    const load = async (init = false) => {
        const data: Status = await api.makeRequest('acme/status', 'GET');
        if (!mounted.current || !data) return;
        setStatus(data);
        if (init) {
            setMethod(data.settings.Method || 'http');
            setTarget(data.settings.Target);
            setEmail(data.settings.Email);
        }
    };
    useEffect(() => {
        mounted.current = true;
        load(true).catch((e) => setError(e.message));
        return () => {
            mounted.current = false;
        };
    }, []);
    useEffect(() => {
        if (!status?.running) return undefined;
        const timer = window.setInterval(() => load().catch((e) => setError(e.message)), 3000);
        return () => window.clearInterval(timer);
    }, [status?.running]);
    const save = async (enabled: boolean) => {
        setSaving(true);
        setError('');
        try {
            await api.makeRequest('acme/config', 'POST', {
                data: {
                    Enabled: enabled,
                    Target: target,
                    Email: email,
                    Method: method,
                    ...(enabled && method === 'cloudflare' && token ? { CFToken: token.trim() } : {}),
                },
            });
            setToken('');
            await load(true);
        } catch (e) {
            setError(String(e));
        } finally {
            setSaving(false);
        }
    };
    const locked = saving || !status?.supported || status.running;
    const cf = method === 'cloudflare';
    const savedToken = status?.hasCFToken && target.trim().toLowerCase() === status.settings.Target;
    return (
        <section className="mb-5">
            <h4>自动申请与续签证书</h4>
            <div className="form-group">
                <label htmlFor="acme-method">证书验证方式</label>
                <select
                    id="acme-method"
                    className="form-control"
                    value={method}
                    disabled={locked}
                    onChange={(e) => {
                        setMethod(e.target.value);
                        setToken('');
                    }}>
                    <option value="http">HTTP（端口 80）</option>
                    <option value="cloudflare">Cloudflare DNS</option>
                </select>
                <small className="form-text text-muted">
                    {cf
                        ? '填写由 Cloudflare 托管 DNS 的单个域名。验证通过 TXT 记录完成。'
                        : '域名须指向本机。公网 TCP 80 须能访问本机，且端口空闲或由 AdGuard Home 监听。'}
                </small>
            </div>
            {cf && (
                <div className="form-group">
                    <label htmlFor="acme-token">Cloudflare API Token</label>
                    <input
                        id="acme-token"
                        type="password"
                        className="form-control"
                        autoComplete="new-password"
                        disabled={locked}
                        value={token}
                        onChange={(e) => setToken(e.target.value)}
                        placeholder={savedToken ? '已保存，留空沿用现有 Token' : ''}
                    />
                    <small className="form-text text-muted">Token 需要目标区域的 Zone Read 和 DNS Edit 权限。</small>
                </div>
            )}
            {status && !status.supported && <p>此功能仅在 Linux 服务器上运行。</p>}
            <div className="form-group">
                <label htmlFor="acme-target">{cf ? '证书域名' : '证书域名或公网 IP'}</label>
                <input
                    id="acme-target"
                    className="form-control"
                    value={target}
                    disabled={locked}
                    onChange={(e) => setTarget(e.target.value)}
                />
            </div>
            <div className="form-group">
                <label htmlFor="acme-email">联系邮箱</label>
                <input
                    id="acme-email"
                    type="email"
                    className="form-control"
                    value={email}
                    disabled={locked}
                    onChange={(e) => setEmail(e.target.value)}
                />
            </div>
            <button
                type="button"
                className="btn btn-primary mr-2 mb-2"
                disabled={locked || !target || !email || (cf && !token.trim() && !savedToken)}
                onClick={() => save(true)}>
                申请并启用自动续签
            </button>
            {status?.settings.Enabled && (
                <button type="button" className="btn btn-secondary mb-2" disabled={locked} onClick={() => save(false)}>
                    停止自动续签
                </button>
            )}
            <p role="status">
                {status?.running
                    ? '正在检查或签发证书…'
                    : status?.message || (status?.settings.Enabled ? '自动续签已启用' : '自动续签未启用')}
            </p>
            {error && (
                <p role="alert" className="text-danger">
                    {error}
                </p>
            )}
            {status?.expires && (
                <div>
                    <p>
                        {status.settings.Target} 的证书到期时间：{new Date(status.expires).toLocaleString('zh-CN')}
                    </p>
                    <button
                        type="button"
                        className="btn btn-outline-primary"
                        onClick={() => usePaths(status.cert, status.key)}>
                        填入下方 TLS 设置
                    </button>
                    <p className="mt-2">首次使用请检查加密开关、端口并保存下方设置。续签后沿用同一路径。</p>
                </div>
            )}
        </section>
    );
};
