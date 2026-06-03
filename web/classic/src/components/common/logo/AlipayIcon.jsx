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

import React from 'react';
import { Icon } from '@douyinfe/semi-ui';

const AlipayIcon = () => {
  function CustomIcon() {
    return (
      <svg
        className='icon'
        viewBox='0 0 1024 1024'
        version='1.1'
        xmlns='http://www.w3.org/2000/svg'
        width='20'
        height='20'
      >
        <path
          d='M789.2 569.1c-37.2-14.6-79.9-28.7-126.1-41.7 21.5-69.4 34.3-146.8 34.3-220.5 0-33.5-4.1-63.7-12.2-89.7-8.3-26.7-21.3-49.6-38.8-67.1-17.6-17.6-39.9-29.6-64.6-34.6-8.7-1.8-17.6-2.7-26.6-2.7-22.6 0-45 6.2-64.8 18.1-22.3 13.2-39.8 33.2-50.7 57.9-11.5 25.9-16.9 55.9-16.1 89.2 1.1 46.5 13.1 101.5 34.6 159.2-52.6 14.8-102.3 33.4-147.7 55.2-50.7 24.4-94.7 52.8-130.8 84.4-39.6 34.7-68.8 72.8-86.6 113.2-9.3 21-15.5 42.8-18.6 65.3l-0.5 4.1c-0.5 3.4 0.4 6.8 2.5 9.5 2 2.6 5 4.3 8.3 4.6h1.2c3 0 5.9-1.1 8.2-3.1 2.5-2.2 4.7-4.7 6.5-7.5 18.2-28.3 43.3-54.6 74.7-78.2 31.1-23.4 67.8-44.5 109-62.7 40.9-18.1 86.3-33.5 135-45.8 23.7 56.3 54.1 108.7 89.8 154.9 3.3 4.2 7.7 7.5 12.8 9.4 5.1 1.9 10.6 2.4 15.9 1.3 5.3-1 10.2-3.5 14.2-7.1 4-3.7 6.9-8.4 8.4-13.6 1.5-5.3 1.5-10.9 0-16.1-1.5-5.3-4.4-10-8.4-13.6-3.3-3-6.4-6.4-9.1-10-30.1-40.3-56.2-84.9-77.4-132.6 46.2-11 91.9-18.9 135.6-23.4 50.7-5.2 97.6-6.1 139.4-2.7 44.2 3.6 82.7 12.1 114.3 25.3 34.3 14.3 59.7 33.7 75.3 57.8 8.6 13.2 14 27.8 16.2 43.2 2.1 14.7 1.2 29.8-2.7 44.3-4 14.9-11 28.8-20.5 40.7-9.8 12.3-22 22.5-35.8 29.9-15.1 8.1-31.8 13-49 14.5l-3.4 0.3c-3.1 0.3-5.9 1.8-7.9 4.1-2 2.4-3 5.4-2.8 8.5 0.2 3.1 1.5 6 3.8 8.1 2.2 2 5.1 3.2 8.1 3.2h0.5c22.3-1.5 44-7.8 63.8-18.5 19-10.3 35.7-24.4 49.1-41.5 13.2-16.8 22.9-36 28.6-56.6 5.6-20.1 7.2-41.3 4.6-62.1-2.7-21.6-9.8-42.4-21.1-61.3-14.7-24.7-36.1-45.7-63.7-62.3z'
          fill='#1677FF'
        ></path>
      </svg>
    );
  }

  return (
    <div>
      <Icon svg={<CustomIcon />} />
    </div>
  );
};

export default AlipayIcon;
