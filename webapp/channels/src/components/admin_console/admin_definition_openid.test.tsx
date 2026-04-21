// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import type {ClientLicense} from '@mattermost/types/config';

import {RESOURCE_KEYS} from 'mattermost-redux/constants/permissions_sysconsole';

import {Constants} from 'utils/constants';

import AdminDefinition from './admin_definition';
import type {AdminDefinitionSettingDropdownOption, ConsoleAccess} from './types';

type IsHiddenCheck = (
    config: object,
    state: object,
    license?: ClientLicense,
    enterpriseReady?: boolean,
    consoleAccess?: ConsoleAccess,
) => boolean;

describe('AdminDefinition - OpenID settings', () => {
    const consoleAccess = {
        read: {
            [RESOURCE_KEYS.AUTHENTICATION.OPENID]: true,
        },
        write: {},
    } as unknown as ConsoleAccess;

    const getOpenIdSection = () => AdminDefinition.authentication.subsections.openid;
    const getOpenIdFeatureDiscoverySection = () => AdminDefinition.authentication.subsections.openid_feature_discovery;

    const getOpenIdTypeOptions = () => {
        const settings = 'settings' in getOpenIdSection().schema! ? getOpenIdSection().schema.settings : [];
        const openIdTypeSetting = settings.find((setting) => setting.key === 'openidType');
        expect(openIdTypeSetting).toBeDefined();
        expect(openIdTypeSetting?.type).toBe('dropdown');

        return openIdTypeSetting?.options || [];
    };

    const getOption = (value: string): AdminDefinitionSettingDropdownOption => {
        const option = getOpenIdTypeOptions().find((candidate) => candidate.value === value);
        expect(option).toBeDefined();
        return option!;
    };

    test('shows the OpenID admin page without an OpenId license', () => {
        const unlicensed = {IsLicensed: 'false'} as ClientLicense;
        const isHidden = getOpenIdSection().isHidden as IsHiddenCheck;

        expect(isHidden({}, {}, unlicensed, false, consoleAccess)).toBe(false);
    });

    test('keeps only the custom OpenID provider visible without an OpenId license', () => {
        const unlicensed = {IsLicensed: 'false'} as ClientLicense;
        const gitlabOption = getOption(Constants.GITLAB_SERVICE);
        const googleOption = getOption(Constants.GOOGLE_SERVICE);
        const office365Option = getOption(Constants.OFFICE365_SERVICE);
        const openIdOption = getOption(Constants.OPENID_SERVICE);

        expect((gitlabOption.isHidden as IsHiddenCheck)({}, {}, unlicensed)).toBe(true);
        expect((googleOption.isHidden as IsHiddenCheck)({}, {}, unlicensed)).toBe(true);
        expect((office365Option.isHidden as IsHiddenCheck)({}, {}, unlicensed)).toBe(true);
        expect(openIdOption.isHidden).toBeUndefined();
    });

    test('keeps the standalone feature discovery route hidden', () => {
        const isHidden = getOpenIdFeatureDiscoverySection().isHidden as IsHiddenCheck;

        expect(isHidden({}, {})).toBe(true);
    });
});
