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

import React, { useEffect, useState, useRef } from 'react';
import { Banner, Button, Form, Row, Col, Spin } from '@douyinfe/semi-ui';
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';
import { Info } from 'lucide-react';

export default function SettingsPaymentGatewayAlipayDirect(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle
    ? undefined
    : t('支付宝官方设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    AlipayDirectEnabled: false,
    AlipayDirectAppId: '',
    AlipayDirectPrivateKey: '',
    AlipayDirectPublicKey: '',
    AlipayDirectSandbox: false,
    AlipayDirectNotifyUrl: '',
    AlipayDirectReturnUrl: '',
    AlipayDirectMinTopUp: 1,
  });
  const [originInputs, setOriginInputs] = useState({});
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        AlipayDirectEnabled:
          props.options.AlipayDirectEnabled !== undefined
            ? props.options.AlipayDirectEnabled
            : false,
        AlipayDirectAppId: props.options.AlipayDirectAppId || '',
        AlipayDirectPrivateKey: props.options.AlipayDirectPrivateKey || '',
        AlipayDirectPublicKey: props.options.AlipayDirectPublicKey || '',
        AlipayDirectSandbox:
          props.options.AlipayDirectSandbox !== undefined
            ? props.options.AlipayDirectSandbox
            : false,
        AlipayDirectNotifyUrl: props.options.AlipayDirectNotifyUrl || '',
        AlipayDirectReturnUrl: props.options.AlipayDirectReturnUrl || '',
        AlipayDirectMinTopUp:
          props.options.AlipayDirectMinTopUp !== undefined
            ? parseFloat(props.options.AlipayDirectMinTopUp)
            : 1,
      };
      setInputs(currentInputs);
      setOriginInputs({ ...currentInputs });
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const handleFormChange = (values) => {
    setInputs(values);
  };

  const submitAlipayDirectSetting = async () => {
    if (props.options.ServerAddress === '') {
      showError(t('请先填写服务器地址'));
      return;
    }

    setLoading(true);
    try {
      const options = [];

      if (
        originInputs['AlipayDirectEnabled'] !== inputs.AlipayDirectEnabled &&
        inputs.AlipayDirectEnabled !== undefined
      ) {
        options.push({
          key: 'AlipayDirectEnabled',
          value: inputs.AlipayDirectEnabled ? 'true' : 'false',
        });
      }
      if (inputs.AlipayDirectAppId !== '') {
        options.push({
          key: 'AlipayDirectAppId',
          value: inputs.AlipayDirectAppId,
        });
      }
      if (inputs.AlipayDirectPrivateKey !== '') {
        options.push({
          key: 'AlipayDirectPrivateKey',
          value: inputs.AlipayDirectPrivateKey,
        });
      }
      if (inputs.AlipayDirectPublicKey !== '') {
        options.push({
          key: 'AlipayDirectPublicKey',
          value: inputs.AlipayDirectPublicKey,
        });
      }
      if (
        originInputs['AlipayDirectSandbox'] !== inputs.AlipayDirectSandbox &&
        inputs.AlipayDirectSandbox !== undefined
      ) {
        options.push({
          key: 'AlipayDirectSandbox',
          value: inputs.AlipayDirectSandbox ? 'true' : 'false',
        });
      }
      if (inputs.AlipayDirectNotifyUrl !== undefined) {
        options.push({
          key: 'AlipayDirectNotifyUrl',
          value: removeTrailingSlash(inputs.AlipayDirectNotifyUrl),
        });
      }
      if (inputs.AlipayDirectReturnUrl !== undefined) {
        options.push({
          key: 'AlipayDirectReturnUrl',
          value: removeTrailingSlash(inputs.AlipayDirectReturnUrl),
        });
      }
      if (
        inputs.AlipayDirectMinTopUp !== undefined &&
        inputs.AlipayDirectMinTopUp !== null
      ) {
        options.push({
          key: 'AlipayDirectMinTopUp',
          value: inputs.AlipayDirectMinTopUp.toString(),
        });
      }

      const requestQueue = options.map((opt) =>
        API.put('/api/option/', {
          key: opt.key,
          value: opt.value,
        }),
      );

      const results = await Promise.all(requestQueue);

      const errorResults = results.filter((res) => !res.data.success);
      if (errorResults.length > 0) {
        errorResults.forEach((res) => {
          showError(res.data.message);
        });
      } else {
        showSuccess(t('更新成功'));
        setOriginInputs({ ...inputs });
        props.refresh?.();
      }
    } catch (error) {
      showError(t('更新失败'));
    }
    setLoading(false);
  };

  const serverAddress = props.options.ServerAddress
    ? removeTrailingSlash(props.options.ServerAddress)
    : t('网站地址');

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={handleFormChange}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={sectionTitle}>
          <Banner
            type='info'
            icon={<Info size={16} />}
            description={
              <>
                {t(
                  '使用支付宝开放平台官方接口（电脑网站支付 TradePagePay），需先在支付宝开放平台创建应用并获取 AppID 和密钥。',
                )}
                <br />
                {t('回调地址')}：{serverAddress}/api/alipay-direct/webhook
                <br />
                {t('同步回调地址')}：{serverAddress}
                /api/subscription/alipay-direct/return
              </>
            }
            style={{ marginBottom: 16 }}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='AlipayDirectEnabled'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('启用支付宝官方支付')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='AlipayDirectSandbox'
                size='default'
                checkedText='｜'
                uncheckedText='〇'
                label={t('沙箱模式')}
                extraText={t('启用后将使用支付宝沙箱环境进行测试')}
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayDirectAppId'
                label={t('应用 AppID')}
                placeholder={t('例如：2021xxx')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayDirectPrivateKey'
                label={t('应用私钥')}
                placeholder={t('RSA2 私钥，留空表示保持当前不变')}
                extraText={t('保存后不会回显')}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayDirectPublicKey'
                label={t('支付宝公钥')}
                placeholder={t('支付宝公钥，留空表示保持当前不变')}
                extraText={t('保存后不会回显')}
                type='password'
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayDirectNotifyUrl'
                label={t('异步通知地址')}
                placeholder={t('留空则使用默认地址')}
                extraText={t('支付宝服务器主动通知的地址')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='AlipayDirectReturnUrl'
                label={t('同步回调地址')}
                placeholder={t('留空则使用默认地址')}
                extraText={t('支付完成后跳转的地址')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.InputNumber
                field='AlipayDirectMinTopUp'
                label={t('最低充值美元数量')}
                placeholder={t('例如：1，就是最低充值1$')}
                extraText={t('用户单次最少可充值的美元数量')}
              />
            </Col>
          </Row>
          <Button
            onClick={submitAlipayDirectSetting}
            style={{ marginTop: 16 }}
          >
            {t('更新支付宝官方设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
