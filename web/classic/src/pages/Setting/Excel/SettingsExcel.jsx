import React, { useEffect, useState, useRef } from 'react';
import {
  Banner,
  Button,
  Col,
  Form,
  InputNumber,
  Row,
  Spin,
  TextArea,
} from '@douyinfe/semi-ui';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsExcel(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    'excel_tmp_key.enabled': false,
    'excel_tmp_key.account': '',
    'excel_tmp_key.expire_days': 7,
    'excel_tmp_key.quota': 500000,
    'excel_version_check.minimum_versions': '{}',
  });
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function handleFieldChange(fieldName) {
    return (value) => {
      setInputs((inputs) => ({ ...inputs, [fieldName]: value }));
    };
  }

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow);
    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = String(inputs[item.key]);
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        if (typeof inputs[key] === 'boolean') {
          currentInputs[key] =
            props.options[key] === 'true' || props.options[key] === true;
        } else if (typeof inputs[key] === 'number') {
          currentInputs[key] = parseInt(props.options[key]) || inputs[key];
        } else {
          currentInputs[key] = props.options[key];
        }
      }
    }
    setInputs({ ...inputs, ...currentInputs });
    setInputsRow({ ...inputs, ...currentInputs });
    if (refForm.current) {
      refForm.current.setValues({ ...inputs, ...currentInputs });
    }
  }, [props.options]);

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
        >
          <Form.Section text={t('Excel 临时令牌')}>
            <Banner
              type='info'
              description={t(
                '允许未认证用户通过 Excel 插件创建临时 API 令牌',
              )}
              style={{ marginBottom: 16 }}
            />
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'excel_tmp_key.enabled'}
                  label={t('启用临时令牌')}
                  extraText={t('允许 Excel 插件创建临时 API 令牌')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={handleFieldChange('excel_tmp_key.enabled')}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Input
                  field={'excel_tmp_key.account'}
                  label={t('公共账户')}
                  extraText={t('拥有临时令牌的账户用户名')}
                  placeholder={t('输入用户名')}
                  onChange={handleFieldChange('excel_tmp_key.account')}
                  showClear
                  disabled={!inputs['excel_tmp_key.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'excel_tmp_key.expire_days'}
                  label={t('过期天数')}
                  extraText={t('临时令牌过期天数')}
                  min={1}
                  onChange={handleFieldChange('excel_tmp_key.expire_days')}
                  disabled={!inputs['excel_tmp_key.enabled']}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.InputNumber
                  field={'excel_tmp_key.quota'}
                  label={t('令牌额度')}
                  extraText={t('每个临时令牌的额度（内部单位）')}
                  min={0}
                  onChange={handleFieldChange('excel_tmp_key.quota')}
                  disabled={!inputs['excel_tmp_key.enabled']}
                />
              </Col>
            </Row>
          </Form.Section>

          <Form.Section text={t('Excel 版本检查')}>
            <Banner
              type='info'
              description={t(
                '配置客户端最低版本要求，低于该版本的客户端将收到更新提示。格式为 JSON，例如 {"excel-plugin":"1.2.0","ai-sdk":"2.0.0"}',
              )}
              style={{ marginBottom: 16 }}
            />
            <Row gutter={16}>
              <Col span={24}>
                <Form.TextArea
                  field={'excel_version_check.minimum_versions'}
                  label={t('最低版本配置')}
                  placeholder={'{"excel-plugin":"1.2.0","ai-sdk":"2.0.0"}'}
                  rows={4}
                  onChange={handleFieldChange(
                    'excel_version_check.minimum_versions',
                  )}
                />
              </Col>
            </Row>
          </Form.Section>

          <Row>
            <Button size='default' onClick={onSubmit}>
              {t('保存 Excel 设置')}
            </Button>
          </Row>
        </Form>
      </Spin>
    </>
  );
}
