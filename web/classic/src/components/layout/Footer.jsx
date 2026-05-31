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

import React, { useEffect, useState, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import { Typography } from '@douyinfe/semi-ui';
import { getFooterHTML, getSystemName } from '../../helpers';
import { StatusContext } from '../../context/Status';

const FooterBar = () => {
  const { t } = useTranslation();
  const [footer, setFooter] = useState(getFooterHTML());
  const systemName = getSystemName();
  const [statusState] = useContext(StatusContext);
  const status = statusState?.status;

  const loadFooter = () => {
    let footer_html = localStorage.getItem('footer_html');
    if (footer_html) {
      setFooter(footer_html);
    }
  };

  const currentYear = new Date().getFullYear();

  // Build legal links from status config
  const legalItems = [];
  if (status?.user_agreement_enabled) {
    legalItems.push({ label: t('用户协议'), href: '/user-agreement' });
  }
  if (status?.privacy_policy_enabled) {
    legalItems.push({ label: t('隐私政策'), href: '/privacy-policy' });
  }
  if (status?.terms_of_service_enabled) {
    legalItems.push({ label: t('服务条款'), href: '/terms-of-service' });
  }

  useEffect(() => {
    loadFooter();
  }, []);

  return (
    <div className='w-full'>
      {footer ? (
        <footer className='relative h-auto py-4 px-6 md:px-24 w-full flex items-center justify-center overflow-hidden'>
          <div className='flex flex-col md:flex-row items-center justify-between w-full max-w-[1110px] gap-4'>
            <div
              className='custom-footer na-cb6feaffe3990c78 text-sm !text-semi-color-text-1'
              dangerouslySetInnerHTML={{ __html: footer }}
            />
            {legalItems.length > 0 && (
              <div className='text-sm flex-shrink-0 flex items-center gap-3'>
                {legalItems.map((item, index) => (
                  <React.Fragment key={item.href}>
                    {index > 0 && (
                      <span className='!text-semi-color-text-2'>·</span>
                    )}
                    <a
                      href={item.href}
                      className='!text-semi-color-text-1 hover:!text-semi-color-text-0 transition-colors'
                    >
                      {item.label}
                    </a>
                  </React.Fragment>
                ))}
              </div>
            )}
          </div>
        </footer>
      ) : (
        <footer className='relative h-auto py-10 px-6 md:px-24 w-full flex flex-col items-center justify-center overflow-hidden'>
          <div className='flex flex-col items-center justify-between w-full max-w-[1110px] gap-6'>
            <div className='flex flex-col items-center gap-2'>
              <Typography.Text className='text-sm !text-semi-color-text-1'>
                &copy; {currentYear} {systemName}. {t('版权所有')}
              </Typography.Text>
            </div>
            {legalItems.length > 0 && (
              <div className='flex flex-wrap items-center justify-center gap-3'>
                {legalItems.map((item, index) => (
                  <React.Fragment key={item.href}>
                    {index > 0 && (
                      <span className='!text-semi-color-text-2'>·</span>
                    )}
                    <a
                      href={item.href}
                      className='text-sm !text-semi-color-text-1 hover:!text-semi-color-text-0 transition-colors'
                    >
                      {item.label}
                    </a>
                  </React.Fragment>
                ))}
              </div>
            )}
          </div>
        </footer>
      )}
    </div>
  );
};

export default FooterBar;
