/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Modal, Spin, Image } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { SiWechat } from 'react-icons/si';
import { useTranslation } from 'react-i18next';
import { getWeChatMpQrCode, getWeChatMpStatus } from '../../helpers';

const POLL_INTERVAL_MS = 2000;

/**
 * WeChat Scan Login Modal (classic frontend).
 *
 * Props:
 *   - visible: boolean
 *   - onCancel: () => void
 *   - onSuccess: (userData) => void  // called with the logged_in data payload
 *   - affCode: string (optional)
 */
const WeChatMpLoginModal = ({ visible, onCancel, onSuccess, affCode }) => {
  const { t } = useTranslation();
  const [status, setStatus] = useState('loading'); // loading | pending | expired | error
  const [qrUrl, setQrUrl] = useState('');
  const [errorMessage, setErrorMessage] = useState('');

  const sceneIdRef = useRef('');
  const pollTimerRef = useRef(null);
  const expireTimerRef = useRef(null);
  const isHandlingLoginRef = useRef(false);
  const isFetchingQrRef = useRef(false);

  const clearTimers = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    if (expireTimerRef.current) {
      clearTimeout(expireTimerRef.current);
      expireTimerRef.current = null;
    }
  }, []);

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current) {
      clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  const poll = useCallback(async () => {
    const sceneId = sceneIdRef.current;
    if (!sceneId) return;
    const res = await getWeChatMpStatus(sceneId);
    if (!res || !res.data) return;
    const data = res.data;
    if (data.status === 'logged_in') {
      if (isHandlingLoginRef.current) return;
      isHandlingLoginRef.current = true;
      stopPolling();
      try {
        if (typeof onSuccess === 'function') {
          await onSuccess(data);
        }
      } finally {
        isHandlingLoginRef.current = false;
      }
      return;
    }
    if (data.status === 'expired' || data.status === 'failed') {
      stopPolling();
      setStatus('expired');
      return;
    }
    // pending / scanned continue polling
    setStatus((prev) => (prev === 'pending' ? prev : 'pending'));
  }, [onSuccess, stopPolling]);

  const fetchQrCode = useCallback(async () => {
    if (isFetchingQrRef.current) return;
    isFetchingQrRef.current = true;
    clearTimers();
    setStatus('loading');
    setQrUrl('');
    setErrorMessage('');
    sceneIdRef.current = '';

    try {
      const data = await getWeChatMpQrCode(affCode);
      if (!data) {
        setStatus('error');
        setErrorMessage(t('生成二维码失败'));
        return;
      }
      sceneIdRef.current = data.scene_id;
      setQrUrl(data.qr_url);
      setStatus('pending');

      pollTimerRef.current = setInterval(poll, POLL_INTERVAL_MS);
      void poll();

      const ttl = Math.max((data.expire_seconds - 2) * 1000, 10 * 1000);
      expireTimerRef.current = setTimeout(() => {
        stopPolling();
        setStatus('expired');
      }, ttl);
    } catch (err) {
      setStatus('error');
      setErrorMessage(err?.message || t('生成二维码失败'));
    } finally {
      isFetchingQrRef.current = false;
    }
  }, [affCode, clearTimers, poll, stopPolling, t]);

  useEffect(() => {
    if (visible) {
      void fetchQrCode();
    } else {
      clearTimers();
      sceneIdRef.current = '';
      setStatus('loading');
      setQrUrl('');
      setErrorMessage('');
      isHandlingLoginRef.current = false;
      isFetchingQrRef.current = false;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible]);

  useEffect(() => {
    return () => {
      clearTimers();
    };
  }, [clearTimers]);

  const handleRefresh = () => {
    void fetchQrCode();
  };

  return (
    <Modal
      title={
        <div className='flex items-center'>
          <SiWechat className='mr-2 text-green-500' size={20} />
          {t('微信扫码登录')}
        </div>
      }
      visible={visible}
      onCancel={onCancel}
      footer={null}
      size={'small'}
      centered={true}
      className='modern-modal'
    >
      <div className='space-y-4 py-4 text-center min-h-[16rem] flex flex-col items-center justify-center'>
        {status === 'loading' && (
          <>
            <Spin size='large' />
            <div className='text-gray-600'>{t('正在生成二维码...')}</div>
          </>
        )}

        {status === 'pending' && qrUrl && (
          <>
            <Image
              src={qrUrl}
              alt={t('微信登录二维码')}
              className='mx-auto'
              width={200}
              height={200}
            />
            <div className='text-gray-600'>
              {t('请使用微信扫描二维码登录')}
            </div>
            <div className='text-gray-400 text-sm'>{t('等待扫码...')}</div>
          </>
        )}

        {status === 'expired' && (
          <>
            <div className='text-gray-600'>{t('二维码已过期')}</div>
            <Button
              theme='solid'
              icon={<IconRefresh />}
              onClick={handleRefresh}
              className='!rounded-lg !bg-slate-600 hover:!bg-slate-700'
            >
              {t('刷新二维码')}
            </Button>
          </>
        )}

        {status === 'error' && (
          <>
            <div className='text-gray-600'>
              {errorMessage || t('生成二维码失败')}
            </div>
            <Button
              theme='solid'
              icon={<IconRefresh />}
              onClick={handleRefresh}
              className='!rounded-lg !bg-slate-600 hover:!bg-slate-700'
            >
              {t('重试')}
            </Button>
          </>
        )}
      </div>
    </Modal>
  );
};

export default WeChatMpLoginModal;
