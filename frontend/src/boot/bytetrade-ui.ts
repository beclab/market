import { boot } from 'quasar/wrappers';
import BytetradeUi from '@bytetrade/ui';
import { BtNotify, BtDialog } from '@bytetrade/ui';
import { Notify, Dialog } from 'quasar';

export default boot(({ app }) => {
	app.use(BytetradeUi);
	BtNotify.init(Notify);
	BtDialog.init(Dialog);
});

export { BytetradeUi };
