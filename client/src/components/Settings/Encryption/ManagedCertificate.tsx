import React, { useEffect, useRef, useState } from 'react';
import api from '../../../api/Api';

type Settings = { Enabled: boolean; Target: string; Email: string };
type Status = {
    settings: Settings;
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
    const [email, setEmail] = useState('');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const mounted = useRef(false);
    const load = async (init = false) => {
        const data: Status = await api.makeRequest('acme/status', 'GET');
        if (!mounted.current || !data) return;
        setStatus(data);
        if (init) {
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
            await api.makeRequest('acme/config', 'POST', { data: { Enabled: enabled, Target: target, Email: email } });
            await load(true);
        } catch (e) {
            setError(String(e));
        } finally {
            setSaving(false);
        }
    };
    const locked = saving || !status?.supported || status.running;
    return (
        <section className="mb-5">
            <h4>自动申请与续签证书</h4>
            <p>
                由 Let’s Encrypt 签发，acme.sh 自动续签。填写单个域名或公网 IP；域名须指向本机。公网 TCP 80
                必须能访问本机，且端口空闲或由 AdGuard Home 监听。
            </p>
            {status && !status.supported && <p>此功能仅在 Linux 服务器上运行。</p>}
            <div className="form-group">
                <label htmlFor="acme-target">证书域名或公网 IP</label>
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
                disabled={locked || !target || !email}
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
