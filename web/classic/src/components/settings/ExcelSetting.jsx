import React, { useEffect, useState } from 'react';
import { Card, Spin } from '@douyinfe/semi-ui';
import SettingsExcel from '../../pages/Setting/Excel/SettingsExcel';
import { API, showError, toBoolean } from '../../helpers';

const ExcelSetting = () => {
  let [inputs, setInputs] = useState({
    'excel_tmp_key.enabled': false,
    'excel_tmp_key.account': '',
    'excel_tmp_key.expire_days': 7,
    'excel_tmp_key.quota': 500000,
    'excel_version_check.minimum_versions': '{}',
    'excel_model_list.models': '[]',
  });

  let [loading, setLoading] = useState(false);

  const getOptions = async () => {
    const res = await API.get('/api/option/');
    const { success, message, data } = res.data;
    if (success) {
      let newInputs = {};
      data.forEach((item) => {
        if (typeof inputs[item.key] === 'boolean') {
          newInputs[item.key] = toBoolean(item.value);
        } else if (typeof inputs[item.key] === 'number') {
          newInputs[item.key] = parseInt(item.value) || inputs[item.key];
        } else {
          newInputs[item.key] = item.value;
        }
      });
      setInputs(newInputs);
    } else {
      showError(message);
    }
  };

  async function onRefresh() {
    try {
      setLoading(true);
      await getOptions();
    } catch (error) {
      showError(error);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    onRefresh();
  }, []);

  return (
    <>
      <Spin spinning={loading} size='large'>
        <Card style={{ marginTop: '10px' }}>
          <SettingsExcel options={inputs} refresh={onRefresh} />
        </Card>
      </Spin>
    </>
  );
};

export default ExcelSetting;
