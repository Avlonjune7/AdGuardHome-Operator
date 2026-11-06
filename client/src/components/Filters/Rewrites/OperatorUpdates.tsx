import React, { useEffect, useState } from 'react';
import api from '../../../api/Api';
import type { RewritesData } from '../../../initialState';

type Props = {
    db: RewritesData['settings']['operator_db'];
    refresh: () => void;
};

const localTime = (time: string) => new Date(time).toLocaleString('zh-CN');

export const OperatorUpdates = ({ db, refresh }: Props) => {
    const [edit, setEdit] = useState<string | null>(null);
    const [pending, setPending] = useState(false);
    const [feedback, setFeedback] = useState('');
    const hours = db?.interval_hours ?? 24;
    const value = edit ?? String(hours);
    const valid = /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 720;
    useEffect(() => {
        if (!db?.running) return undefined;
        const timer = window.setInterval(refresh, 2000);
        return () => window.clearInterval(timer);
    }, [db?.running, refresh]);

    const submit = async (manual: boolean) => {
        setPending(true);
        setFeedback('');
        try {
            if (manual) {
                await api.makeRequest('rewrite/operator/refresh', 'POST', { data: {} });
            } else {
                await api.makeRequest('rewrite/operator/interval', 'POST', { data: { interval_hours: Number(value) } });
                setEdit(null);
                setFeedback('更新周期已保存。');
            }
            refresh();
        } catch (error) {
            setFeedback(String(error));
        } finally {
            setPending(false);
        }
    };
    let statusText = db?.ready ? `最近成功更新：${localTime(db.updated)}` : '暂无可用数据库。';
    if (db?.running) statusText = '正在更新…';
    return (
        <section className="card mb-3" aria-labelledby="operator-db-title">
            <div className="card-body">
                <h3 id="operator-db-title" className="card-title">
                    运营商 IP 库
                </h3>
                <p role="status">
                    {statusText}
                    {db?.error && <span className="d-block text-danger">最近错误：{db.error}</span>}
                </p>
                <div className="form-group">
                    <label htmlFor="operator-interval">自动更新周期（小时）</label>
                    <div className="d-flex flex-wrap align-items-center">
                        <input
                            id="operator-interval"
                            type="number"
                            min="1"
                            max="720"
                            step="1"
                            className="form-control mr-2 mb-2"
                            style={{ width: '7rem' }}
                            value={value}
                            onChange={(e) => setEdit(e.target.value)}
                            aria-invalid={!valid}
                        />
                        <button
                            type="button"
                            className="btn btn-outline-primary mr-2 mb-2"
                            disabled={!db || pending || !valid || Number(value) === hours}
                            onClick={() => submit(false)}>
                            保存周期
                        </button>
                        <button
                            type="button"
                            className="btn btn-primary mb-2"
                            disabled={!db || pending || db.running}
                            onClick={() => submit(true)}>
                            立即更新
                        </button>
                    </div>
                    <small className="text-muted">启用运营商规则后按周期更新，默认 24 小时，可设 1–720 小时。</small>
                </div>
                {feedback && <p role="status">{feedback}</p>}
                <details>
                    <summary>更新日志（最近 {db?.history?.length ?? 0} 条）</summary>
                    {db?.history?.length ? (
                        <ul className="mt-2 pl-4">
                            {db.history.map((entry) => (
                                <li key={entry.at}>
                                    {localTime(entry.at)} · {entry.manual ? '手动' : '自动'} ·{' '}
                                    {entry.error ? `失败：${  entry.error}` : '成功'}
                                </li>
                            ))}
                        </ul>
                    ) : (
                        <p className="mt-2 mb-0">下次更新后会在这里记录结果。</p>
                    )}
                </details>
            </div>
        </section>
    );
};
