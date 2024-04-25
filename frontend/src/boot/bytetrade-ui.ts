import { boot } from 'quasar/wrappers';
import BytetradeUi from '@bytetrade/ui';
import { BtNotify } from '@bytetrade/ui';
import { Notify } from 'quasar';

export default boot(({ app }) => {
	app.use(BytetradeUi);
	BtNotify.init(Notify);
});

export { BytetradeUi };
